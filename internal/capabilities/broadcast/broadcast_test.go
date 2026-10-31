package broadcast

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"net"
	"sync"
	"testing"
	"time"
)

func generateRandomPayload(size int) []byte {
	buf := make([]byte, size)
	_, _ = crand.Read(buf)
	return buf
}

func TestChunkingAndReassembly(t *testing.T) {
	t.Parallel()

	originalPayload := generateRandomPayload(64 * 1024) // 64 KB screen payload
	frame := ScreenFrame{
		Sequence:       42,
		Timestamp:      time.Now().Truncate(time.Millisecond),
		SourceDeviceID: "teacher-host-01",
		Width:          1920,
		Height:         1080,
		Payload:        originalPayload,
	}

	chunks, err := ChunkFrame(frame, 1200)
	if err != nil {
		t.Fatalf("ChunkFrame failed: %v", err)
	}

	expectedChunks := (len(originalPayload) + 1200 - 1) / 1200
	if len(chunks) != expectedChunks {
		t.Fatalf("expected %d chunks, got %d", expectedChunks, len(chunks))
	}

	// Verify each chunk fits in MTU <= 1400 bytes and has valid CRC
	for i, chunk := range chunks {
		if chunk.ChunkIndex != i {
			t.Errorf("chunk %d has invalid index %d", i, chunk.ChunkIndex)
		}
		if chunk.TotalChunks != expectedChunks {
			t.Errorf("chunk %d has invalid total chunks %d", i, chunk.TotalChunks)
		}
		if !chunk.VerifyCRC() {
			t.Errorf("chunk %d failed CRC verification", i)
		}

		packet, err := chunk.MarshalBinary()
		if err != nil {
			t.Fatalf("chunk %d marshal failed: %v", i, err)
		}
		if len(packet) > MaxPacketSize {
			t.Errorf("packet %d size %d exceeds MTU limit %d", i, len(packet), MaxPacketSize)
		}

		var decoded PacketChunk
		if err := decoded.UnmarshalBinary(packet); err != nil {
			t.Fatalf("chunk %d unmarshal failed: %v", i, err)
		}
		if decoded.Sequence != frame.Sequence || decoded.ChunkIndex != i {
			t.Errorf("decoded chunk %d mismatch: got seq=%d idx=%d", i, decoded.Sequence, decoded.ChunkIndex)
		}
	}

	// Reassemble sequentially
	reassembler := NewFrameReassembler(2 * time.Second)
	var reassembled *ScreenFrame

	for _, chunk := range chunks {
		var err error
		reassembled, err = reassembler.AddChunk(chunk)
		if err != nil {
			t.Fatalf("AddChunk failed: %v", err)
		}
	}

	if reassembled == nil {
		t.Fatal("expected frame to be fully reassembled, got nil")
	}

	if reassembled.Sequence != frame.Sequence {
		t.Errorf("sequence mismatch: got %d, want %d", reassembled.Sequence, frame.Sequence)
	}
	if reassembled.Width != frame.Width || reassembled.Height != frame.Height {
		t.Errorf("dimensions mismatch: got %dx%d, want %dx%d", reassembled.Width, reassembled.Height, frame.Width, frame.Height)
	}
	if reassembled.SourceDeviceID != frame.SourceDeviceID {
		t.Errorf("source device mismatch: got %s, want %s", reassembled.SourceDeviceID, frame.SourceDeviceID)
	}
	if !bytes.Equal(reassembled.Payload, frame.Payload) {
		t.Fatal("payload content mismatch after reassembly")
	}
}

func TestOutOfOrderReassembly(t *testing.T) {
	t.Parallel()

	originalPayload := generateRandomPayload(30 * 1024) // 30 KB
	frame := ScreenFrame{
		Sequence:       101,
		Timestamp:      time.Now(),
		SourceDeviceID: "student-pc-42",
		Width:          1280,
		Height:         720,
		Payload:        originalPayload,
	}

	chunks, err := ChunkFrame(frame, 1000)
	if err != nil {
		t.Fatalf("ChunkFrame failed: %v", err)
	}

	// Shuffle chunks to simulate arbitrary network reordering
	shuffled := make([]PacketChunk, len(chunks))
	copy(shuffled, chunks)
	for i := range shuffled {
		j := mrand.IntN(len(shuffled))
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	reassembler := NewFrameReassembler(2 * time.Second)
	var finalFrame *ScreenFrame

	for _, chunk := range shuffled {
		res, err := reassembler.AddChunk(chunk)
		if err != nil {
			t.Fatalf("AddChunk failed: %v", err)
		}
		if res != nil {
			finalFrame = res
		}
	}

	if finalFrame == nil {
		t.Fatal("failed to assemble frame with shuffled chunks")
	}
	if !bytes.Equal(finalFrame.Payload, originalPayload) {
		t.Fatal("out-of-order reassembled payload does not match original")
	}
}

func TestPacketLossAndTimeoutEviction(t *testing.T) {
	t.Parallel()

	frame1 := ScreenFrame{
		Sequence:       1,
		Timestamp:      time.Now(),
		SourceDeviceID: "teacher-main",
		Width:          1920,
		Height:         1080,
		Payload:        generateRandomPayload(10 * 1024),
	}

	chunks, err := ChunkFrame(frame1, 1200)
	if err != nil {
		t.Fatalf("ChunkFrame failed: %v", err)
	}
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}

	// Use a very short timeout for eviction test
	reassembler := NewFrameReassembler(50 * time.Millisecond)

	// Feed all chunks except chunk 1 (packet loss)
	for i, chunk := range chunks {
		if i == 1 {
			continue // Drop chunk 1
		}
		res, err := reassembler.AddChunk(chunk)
		if err != nil {
			t.Fatalf("AddChunk failed: %v", err)
		}
		if res != nil {
			t.Fatal("frame should not be complete due to missing chunk")
		}
	}

	if reassembler.ActiveFrames() != 1 {
		t.Fatalf("expected 1 active frame, got %d", reassembler.ActiveFrames())
	}

	// Wait past the timeout to trigger eviction
	time.Sleep(70 * time.Millisecond)
	evicted := reassembler.EvictExpired()
	if evicted != 1 {
		t.Fatalf("expected 1 frame evicted, got %d", evicted)
	}
	if reassembler.ActiveFrames() != 0 {
		t.Fatalf("expected 0 active frames after eviction, got %d", reassembler.ActiveFrames())
	}

	// Now send a brand new complete frame 2
	frame2 := ScreenFrame{
		Sequence:       2,
		Timestamp:      time.Now(),
		SourceDeviceID: "teacher-main",
		Width:          1920,
		Height:         1080,
		Payload:        generateRandomPayload(5 * 1024),
	}
	chunks2, _ := ChunkFrame(frame2, 1200)

	var reassembled2 *ScreenFrame
	for _, chunk := range chunks2 {
		res, err := reassembler.AddChunk(chunk)
		if err != nil {
			t.Fatalf("AddChunk for frame 2 failed: %v", err)
		}
		if res != nil {
			reassembled2 = res
		}
	}

	if reassembled2 == nil {
		t.Fatal("subsequent frame 2 failed to assemble after prior frame loss")
	}
	if !bytes.Equal(reassembled2.Payload, frame2.Payload) {
		t.Fatal("frame 2 payload mismatch")
	}
}

func TestCRC32MismatchDiscard(t *testing.T) {
	t.Parallel()

	frame := ScreenFrame{
		Sequence:       99,
		Timestamp:      time.Now(),
		SourceDeviceID: "teacher-01",
		Width:          800,
		Height:         600,
		Payload:        generateRandomPayload(4000),
	}

	chunks, err := ChunkFrame(frame, 1000)
	if err != nil {
		t.Fatalf("ChunkFrame failed: %v", err)
	}

	reassembler := NewFrameReassembler(1 * time.Second)

	// Feed first valid chunk
	_, err = reassembler.AddChunk(chunks[0])
	if err != nil {
		t.Fatalf("valid chunk failed: %v", err)
	}

	// Corrupt second chunk's payload
	corrupted := chunks[1]
	corrupted.Payload = make([]byte, len(chunks[1].Payload))
	copy(corrupted.Payload, chunks[1].Payload)
	corrupted.Payload[0] ^= 0xFF // Flip bits

	res, err := reassembler.AddChunk(corrupted)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("expected ErrChecksumMismatch, got %v", err)
	}
	if res != nil {
		t.Fatal("corrupted chunk should not complete frame")
	}

	// Now send the uncorrupted chunk 1
	res, err = reassembler.AddChunk(chunks[1])
	if err != nil {
		t.Fatalf("uncorrupted chunk failed: %v", err)
	}
	if res != nil {
		t.Log("Frame completed or waiting for remaining chunks")
	}
}

func TestMulticastSenderAndReceiverLoopback(t *testing.T) {
	t.Parallel()

	// Use loopback packet conns to verify Sender and Receiver without physical multicast
	rConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to bind receiver conn: %v", err)
	}
	defer rConn.Close()

	sConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to bind sender conn: %v", err)
	}
	defer sConn.Close()

	receiver, err := NewMulticastReceiver(MulticastReceiverConfig{
		Conn:               rConn,
		ReassemblerTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewMulticastReceiver failed: %v", err)
	}
	defer receiver.Close()

	sender, err := NewMulticastSender(MulticastSenderConfig{
		TargetAddr: rConn.LocalAddr().String(),
		Conn:       sConn,
		ChunkSize:  1200,
	})
	if err != nil {
		t.Fatalf("NewMulticastSender failed: %v", err)
	}
	defer sender.Close()

	originalPayload := generateRandomPayload(15 * 1024)
	frame := ScreenFrame{
		Sequence:       555,
		Timestamp:      time.Now().Truncate(time.Millisecond),
		SourceDeviceID: "teacher-node",
		Width:          1920,
		Height:         1080,
		Payload:        originalPayload,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Launch receiver in background
	errCh := make(chan error, 1)
	frameCh := make(chan *ScreenFrame, 1)
	go func() {
		recvFrame, err := receiver.ReceiveFrame(ctx)
		if err != nil {
			errCh <- err
			return
		}
		frameCh <- recvFrame
	}()

	// Send frame from sender
	if err := sender.SendFrame(ctx, frame); err != nil {
		t.Fatalf("SendFrame failed: %v", err)
	}

	select {
	case err := <-errCh:
		t.Fatalf("ReceiveFrame failed: %v", err)
	case recv := <-frameCh:
		if recv.Sequence != frame.Sequence {
			t.Errorf("sequence mismatch: got %d, want %d", recv.Sequence, frame.Sequence)
		}
		if recv.SourceDeviceID != frame.SourceDeviceID {
			t.Errorf("source mismatch: got %s, want %s", recv.SourceDeviceID, frame.SourceDeviceID)
		}
		if !bytes.Equal(recv.Payload, frame.Payload) {
			t.Fatal("received payload does not match sent frame payload")
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for frame delivery")
	}
}

func TestBroadcastSessionRoles(t *testing.T) {
	t.Parallel()

	session := NewBroadcastSession("room-101")
	if session.IsActive() {
		t.Fatal("new session should not be active")
	}

	// Starting without teacher ID should fail
	if err := session.StartTeacherBroadcast(""); !errors.Is(err, ErrTeacherIDRequired) {
		t.Fatalf("expected ErrTeacherIDRequired, got %v", err)
	}

	// Normal Teacher Broadcast
	if err := session.StartTeacherBroadcast("teacher-dev-1"); err != nil {
		t.Fatalf("StartTeacherBroadcast failed: %v", err)
	}

	if !session.IsActive() {
		t.Fatal("session should be active")
	}
	if session.Mode() != BroadcastModeTeacher {
		t.Errorf("expected mode %s, got %s", BroadcastModeTeacher, session.Mode())
	}
	if session.ActiveSenderID() != "teacher-dev-1" {
		t.Errorf("expected active sender teacher-dev-1, got %s", session.ActiveSenderID())
	}

	teacherFrame := ScreenFrame{SourceDeviceID: "teacher-dev-1"}
	if err := session.ValidateFrame(teacherFrame); err != nil {
		t.Fatalf("teacher frame should be valid: %v", err)
	}

	impostorFrame := ScreenFrame{SourceDeviceID: "student-dev-99"}
	if err := session.ValidateFrame(impostorFrame); !errors.Is(err, ErrUnauthorizedSender) {
		t.Fatalf("expected ErrUnauthorizedSender for impostor, got %v", err)
	}

	// Switch to Student Demo ("学生演示")
	var roleChangeLogged bool
	session.SetOnModeChanged(func(oldMode, newMode BroadcastMode, activeSender string) {
		if oldMode == BroadcastModeTeacher && newMode == BroadcastModeStudentDemo && activeSender == "student-dev-7" {
			roleChangeLogged = true
		}
	})

	if err := session.SwitchToStudentDemo("student-dev-7"); err != nil {
		t.Fatalf("SwitchToStudentDemo failed: %v", err)
	}
	if !roleChangeLogged {
		t.Fatal("role change callback was not triggered")
	}
	if session.Mode() != BroadcastModeStudentDemo {
		t.Errorf("expected mode %s, got %s", BroadcastModeStudentDemo, session.Mode())
	}
	if session.ActiveSenderID() != "student-dev-7" {
		t.Errorf("expected active sender student-dev-7, got %s", session.ActiveSenderID())
	}

	// Now student frame is authorized; teacher frame is blocked
	studentFrame := ScreenFrame{SourceDeviceID: "student-dev-7"}
	if err := session.ValidateFrame(studentFrame); err != nil {
		t.Fatalf("student demo frame should be valid: %v", err)
	}
	if err := session.ValidateFrame(teacherFrame); !errors.Is(err, ErrUnauthorizedSender) {
		t.Fatalf("expected teacher frame to be blocked during student demo, got %v", err)
	}

	// Switch back to Teacher Broadcast
	if err := session.SwitchToTeacher(); err != nil {
		t.Fatalf("SwitchToTeacher failed: %v", err)
	}
	if session.Mode() != BroadcastModeTeacher {
		t.Errorf("expected mode %s, got %s", BroadcastModeTeacher, session.Mode())
	}
	if session.ActiveSenderID() != "teacher-dev-1" {
		t.Errorf("expected teacher-dev-1 active sender, got %s", session.ActiveSenderID())
	}
	if err := session.ValidateFrame(teacherFrame); err != nil {
		t.Fatalf("teacher frame should be valid again: %v", err)
	}

	// Stop session
	if err := session.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if session.IsActive() {
		t.Fatal("session should be inactive after stop")
	}
	if err := session.ValidateFrame(teacherFrame); !errors.Is(err, ErrSessionNotActive) {
		t.Fatalf("expected ErrSessionNotActive after stop, got %v", err)
	}
	if err := session.SwitchToStudentDemo("student-dev-7"); !errors.Is(err, ErrInvalidModeTransition) {
		t.Fatalf("cannot switch to student demo from stopped session, got %v", err)
	}
}

func Test100ConcurrentStreamsRace(t *testing.T) {
	t.Parallel()

	// High concurrency race test: 100 concurrent workers pushing chunks,
	// switching modes, validating frames, and evicting expired entries.
	const numWorkers = 100
	reassembler := NewFrameReassembler(500 * time.Millisecond)
	session := NewBroadcastSession("room-race")
	_ = session.StartTeacherBroadcast("teacher-core")

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		workerID := i
		go func() {
			defer wg.Done()

			devID := fmt.Sprintf("device-%03d", workerID)
			payload := []byte(fmt.Sprintf("worker-%03d-frame-data-sample-payload", workerID))

			frame := ScreenFrame{
				Sequence:       uint64(1000 + workerID),
				Timestamp:      time.Now(),
				SourceDeviceID: devID,
				Width:          1920,
				Height:         1080,
				Payload:        payload,
			}

			chunks, err := ChunkFrame(frame, 16)
			if err != nil {
				t.Errorf("worker %d ChunkFrame failed: %v", workerID, err)
				return
			}

			// Add chunks concurrently (simulating concurrent packet arrival)
			var assembled *ScreenFrame
			for _, chunk := range chunks {
				res, err := reassembler.AddChunk(chunk)
				if err != nil {
					t.Errorf("worker %d AddChunk failed: %v", workerID, err)
					return
				}
				if res != nil {
					assembled = res
				}
			}

			if assembled == nil {
				t.Errorf("worker %d failed to assemble frame", workerID)
				return
			}
			if !bytes.Equal(assembled.Payload, payload) {
				t.Errorf("worker %d payload mismatch", workerID)
			}

			// Concurrently inspect and validate with session
			_ = session.IsAuthorizedSender(devID)
			_ = session.ValidateFrame(frame)
			_ = session.Snapshot()

			if workerID%10 == 0 {
				_ = session.SwitchToStudentDemo(devID)
				_ = session.SwitchToTeacher()
			}
		}()
	}

	wg.Wait()
}

type mockAddr string

func (a mockAddr) Network() string { return "mock" }
func (a mockAddr) String() string  { return string(a) }

type mockTimeoutError struct{}

func (e *mockTimeoutError) Error() string   { return "i/o timeout" }
func (e *mockTimeoutError) Timeout() bool   { return true }
func (e *mockTimeoutError) Temporary() bool { return true }

type pipePacketConn struct {
	mu           sync.Mutex
	in           chan []byte
	peer         *pipePacketConn
	localAddr    net.Addr
	readDeadline time.Time
	closed       bool
	closeCh      chan struct{}
}

func newPipePacketConnPair() (*pipePacketConn, *pipePacketConn) {
	c1 := &pipePacketConn{
		in:        make(chan []byte, 1024),
		localAddr: mockAddr("127.0.0.1:10001"),
		closeCh:   make(chan struct{}),
	}
	c2 := &pipePacketConn{
		in:        make(chan []byte, 1024),
		localAddr: mockAddr("127.0.0.1:10002"),
		closeCh:   make(chan struct{}),
	}
	c1.peer = c2
	c2.peer = c1
	return c1, c2
}

func (p *pipePacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return 0, nil, net.ErrClosed
	}
	dl := p.readDeadline
	p.mu.Unlock()

	var timer <-chan time.Time
	if !dl.IsZero() {
		dur := time.Until(dl)
		if dur <= 0 {
			return 0, nil, &mockTimeoutError{}
		}
		t := time.NewTimer(dur)
		defer t.Stop()
		timer = t.C
	}

	select {
	case <-p.closeCh:
		return 0, nil, net.ErrClosed
	case <-timer:
		return 0, nil, &mockTimeoutError{}
	case data := <-p.in:
		n := copy(b, data)
		return n, p.peer.localAddr, nil
	}
}

func (p *pipePacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return 0, net.ErrClosed
	}
	p.mu.Unlock()

	data := make([]byte, len(b))
	copy(data, b)
	select {
	case p.peer.in <- data:
		return len(b), nil
	case <-p.peer.closeCh:
		return 0, net.ErrClosed
	}
}

func (p *pipePacketConn) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		close(p.closeCh)
	}
	return nil
}

func (p *pipePacketConn) LocalAddr() net.Addr           { return p.localAddr }
func (p *pipePacketConn) SetDeadline(t time.Time) error { return p.SetReadDeadline(t) }
func (p *pipePacketConn) SetReadDeadline(t time.Time) error {
	p.mu.Lock()
	p.readDeadline = t
	p.mu.Unlock()
	return nil
}
func (p *pipePacketConn) SetWriteDeadline(t time.Time) error { return nil }

func TestInMemoryPacketConnInjection(t *testing.T) {
	t.Parallel()

	sPipe, rPipe := newPipePacketConnPair()
	defer sPipe.Close()
	defer rPipe.Close()

	receiver, err := NewMulticastReceiver(MulticastReceiverConfig{
		Conn:               rPipe,
		ReassemblerTimeout: 1 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewMulticastReceiver with injected pipe failed: %v", err)
	}
	defer receiver.Close()

	sender, err := NewMulticastSender(MulticastSenderConfig{
		TargetAddr: rPipe.LocalAddr().String(),
		Conn:       sPipe,
		ChunkSize:  500,
	})
	if err != nil {
		t.Fatalf("NewMulticastSender with injected pipe failed: %v", err)
	}
	defer sender.Close()

	frame := ScreenFrame{
		Sequence:       777,
		Timestamp:      time.Now(),
		SourceDeviceID: "in-memory-node",
		Width:          1024,
		Height:         768,
		Payload:        generateRandomPayload(3000),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	frameCh, errCh := receiver.Start(ctx)

	if err := sender.SendFrame(ctx, frame); err != nil {
		t.Fatalf("SendFrame on in-memory connection failed: %v", err)
	}

	select {
	case err := <-errCh:
		t.Fatalf("receiver error on injected conn: %v", err)
	case received := <-frameCh:
		if received.Sequence != frame.Sequence {
			t.Errorf("sequence mismatch: got %d, want %d", received.Sequence, frame.Sequence)
		}
		if !bytes.Equal(received.Payload, frame.Payload) {
			t.Fatal("in-memory delivered payload mismatch")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for in-memory delivery")
	}
}

func TestEmptyPayloadFrame(t *testing.T) {
	t.Parallel()

	frame := ScreenFrame{
		Sequence:       888,
		Timestamp:      time.Now(),
		SourceDeviceID: "heartbeat-source",
		Width:          640,
		Height:         480,
		Payload:        nil, // empty payload
	}

	chunks, err := ChunkFrame(frame, 1200)
	if err != nil {
		t.Fatalf("ChunkFrame for empty payload failed: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for empty frame, got %d", len(chunks))
	}

	reassembler := NewFrameReassembler(1 * time.Second)
	res, err := reassembler.AddChunk(chunks[0])
	if err != nil {
		t.Fatalf("AddChunk for empty frame failed: %v", err)
	}
	if res == nil {
		t.Fatal("empty frame should be complete immediately")
	}
	if len(res.Payload) != 0 {
		t.Errorf("expected 0-length payload, got %d bytes", len(res.Payload))
	}
}
