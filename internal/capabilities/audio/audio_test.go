package audio

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestAudioFormat_Validation(t *testing.T) {
	tests := []struct {
		name    string
		format  AudioFormat
		wantErr bool
	}{
		{
			name:    "valid standard 48kHz stereo 16-bit",
			format:  AudioFormat{SampleRate: 48000, Channels: 2, BitsPerSample: 16},
			wantErr: false,
		},
		{
			name:    "valid 44.1kHz mono 16-bit",
			format:  AudioFormat{SampleRate: 44100, Channels: 1, BitsPerSample: 16},
			wantErr: false,
		},
		{
			name:    "invalid sample rate 0",
			format:  AudioFormat{SampleRate: 0, Channels: 2, BitsPerSample: 16},
			wantErr: true,
		},
		{
			name:    "invalid channels 0",
			format:  AudioFormat{SampleRate: 48000, Channels: 0, BitsPerSample: 16},
			wantErr: true,
		},
		{
			name:    "invalid bit depth 12",
			format:  AudioFormat{SampleRate: 48000, Channels: 2, BitsPerSample: 12},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.format.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestAudioFormat_Calculations(t *testing.T) {
	fmtSpec := AudioFormat{
		SampleRate:    48000,
		Channels:      2,
		BitsPerSample: 16,
	}

	if got := fmtSpec.BytesPerSample(); got != 2 {
		t.Errorf("BytesPerSample() = %d, want 2", got)
	}

	if got := fmtSpec.FrameSize(); got != 4 {
		t.Errorf("FrameSize() = %d, want 4", got)
	}

	if got := fmtSpec.BytesPerSecond(); got != 192000 {
		t.Errorf("BytesPerSecond() = %d, want 192000", got)
	}

	dur := 10 * time.Millisecond
	bytes := fmtSpec.DurationToBytes(dur)
	if bytes != 1920 {
		t.Errorf("DurationToBytes(10ms) = %d, want 1920", bytes)
	}

	calcDur := fmtSpec.BytesToDuration(bytes)
	if calcDur != dur {
		t.Errorf("BytesToDuration(%d) = %v, want %v", bytes, calcDur, dur)
	}

	if str := fmtSpec.String(); str != "48000Hz 16-bit 2ch" {
		t.Errorf("String() = %q, want '48000Hz 16-bit 2ch'", str)
	}
}

func TestAudioPacket_MarshalUnmarshal(t *testing.T) {
	fmtSpec := DefaultAudioFormat
	pcm := make([]byte, 1920)
	for i := range pcm {
		pcm[i] = byte(i % 256)
	}

	pkt := NewAudioPacket(42, 12345678, fmtSpec, FlagNone, pcm)

	wire, err := pkt.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}

	expectedLen := AudioPacketHeaderSize + len(pcm)
	if len(wire) != expectedLen {
		t.Fatalf("wire length = %d, want %d", len(wire), expectedLen)
	}

	decoded := &AudioPacket{}
	if err := decoded.UnmarshalBinary(wire); err != nil {
		t.Fatalf("UnmarshalBinary failed: %v", err)
	}

	if decoded.Sequence != 42 {
		t.Errorf("Sequence = %d, want 42", decoded.Sequence)
	}
	if decoded.Timestamp != 12345678 {
		t.Errorf("Timestamp = %d, want 12345678", decoded.Timestamp)
	}
	if decoded.SampleRate != 48000 {
		t.Errorf("SampleRate = %d, want 48000", decoded.SampleRate)
	}
	if decoded.Channels != 2 {
		t.Errorf("Channels = %d, want 2", decoded.Channels)
	}
	if decoded.BitsPerSample != 16 {
		t.Errorf("BitsPerSample = %d, want 16", decoded.BitsPerSample)
	}
	if len(decoded.Data) != len(pcm) {
		t.Fatalf("Data len = %d, want %d", len(decoded.Data), len(pcm))
	}
	for i := range pcm {
		if decoded.Data[i] != pcm[i] {
			t.Fatalf("Data[%d] = %d, want %d", i, decoded.Data[i], pcm[i])
		}
	}

	// Corrupt payload to check CRC32 verification
	wire[AudioPacketHeaderSize+10] ^= 0xFF
	corruptPkt := &AudioPacket{}
	if err := corruptPkt.UnmarshalBinary(wire); !errors.Is(err, ErrPacketCorrupt) {
		t.Errorf("UnmarshalBinary on corrupted packet error = %v, want ErrPacketCorrupt", err)
	}

	// Corrupt magic header
	wire[0] = 'X'
	magicPkt := &AudioPacket{}
	if err := magicPkt.UnmarshalBinary(wire); !errors.Is(err, ErrPacketCorrupt) {
		t.Errorf("UnmarshalBinary on invalid magic error = %v, want ErrPacketCorrupt", err)
	}

	// Truncated packet
	shortPkt := &AudioPacket{}
	if err := shortPkt.UnmarshalBinary(wire[:20]); !errors.Is(err, ErrPacketTooShort) {
		t.Errorf("UnmarshalBinary on short buffer error = %v, want ErrPacketTooShort", err)
	}
}

func TestJitterBuffer_ReorderAndDeduplicate(t *testing.T) {
	fmtSpec := DefaultAudioFormat
	jb := NewJitterBuffer(fmtSpec, 30*time.Millisecond, 32)

	p1 := NewAudioPacket(1, 1000, fmtSpec, FlagNone, []byte("frame-1"))
	p2 := NewAudioPacket(2, 2000, fmtSpec, FlagNone, []byte("frame-2"))
	p3 := NewAudioPacket(3, 3000, fmtSpec, FlagNone, []byte("frame-3"))
	p4 := NewAudioPacket(4, 4000, fmtSpec, FlagNone, []byte("frame-4"))

	// Push out-of-order: 1, 3, 2, 4
	jb.Push(p1)
	jb.Push(p3)
	jb.Push(p2)
	jb.Push(p4)

	// Push duplicate 2
	jb.Push(p2)

	// In jitter buffer, target buffering delay is reached (len >= 3)
	expectedOrder := []string{"frame-1", "frame-2", "frame-3", "frame-4"}
	for i, want := range expectedOrder {
		data, eos, err := jb.Pop()
		if err != nil {
			t.Fatalf("Pop(%d) unexpected error: %v", i, err)
		}
		if eos {
			t.Fatalf("Pop(%d) unexpected EOS", i)
		}
		if string(data) != want {
			t.Fatalf("Pop(%d) = %q, want %q", i, string(data), want)
		}
	}

	// Next pop should return nil (empty buffer)
	data, _, _ := jb.Pop()
	if data != nil {
		t.Errorf("Pop on empty buffer returned data: %v", data)
	}

	received, played, lost, dropped, underruns, _ := jb.Stats()
	if received != 5 { // 4 packets + 1 duplicate
		t.Errorf("Stats received = %d, want 5", received)
	}
	if played != 4 {
		t.Errorf("Stats played = %d, want 4", played)
	}
	if dropped != 1 { // 1 duplicate dropped
		t.Errorf("Stats dropped = %d, want 1", dropped)
	}
	if lost != 0 {
		t.Errorf("Stats lost = %d, want 0", lost)
	}
	if underruns == 0 {
		t.Errorf("Expected underrun on empty buffer pop")
	}
}

func TestJitterBuffer_LossConcealment(t *testing.T) {
	fmtSpec := DefaultAudioFormat
	jb := NewJitterBuffer(fmtSpec, 20*time.Millisecond, 32)

	p1 := NewAudioPacket(10, 1000, fmtSpec, FlagNone, []byte("frame-10"))
	// p11 is lost!
	p12 := NewAudioPacket(12, 3000, fmtSpec, FlagNone, []byte("frame-12"))
	p13 := NewAudioPacket(13, 4000, fmtSpec, FlagNone, []byte("frame-13"))

	jb.Push(p1)
	jb.Push(p12)
	jb.Push(p13)

	// Pop 1: should be frame-10
	d1, _, err := jb.Pop()
	if err != nil || string(d1) != "frame-10" {
		t.Fatalf("Pop 1 = %q, want 'frame-10'", string(d1))
	}

	// Pop 2: packet 11 is missing. Because queue has 12 and 13, it conceals loss with silence!
	d2, _, err := jb.Pop()
	if err != nil {
		t.Fatalf("Pop 2 error: %v", err)
	}
	// Verify silence frame (all bytes zero)
	if len(d2) == 0 {
		t.Fatalf("Pop 2 expected silence concealment frame, got empty")
	}
	for i, b := range d2 {
		if b != 0 {
			t.Fatalf("Silence frame[%d] = %d, want 0", i, b)
		}
	}

	// Pop 3: should be frame-12
	d3, _, err := jb.Pop()
	if err != nil || string(d3) != "frame-12" {
		t.Fatalf("Pop 3 = %q, want 'frame-12'", string(d3))
	}

	// Pop 4: should be frame-13
	d4, _, err := jb.Pop()
	if err != nil || string(d4) != "frame-13" {
		t.Fatalf("Pop 4 = %q, want 'frame-13'", string(d4))
	}

	_, _, lost, _, _, _ := jb.Stats()
	if lost != 1 {
		t.Errorf("Stats lost = %d, want 1", lost)
	}
}

func TestSenderReceiver_LoopbackPackets(t *testing.T) {
	recConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket failed: %v", err)
	}
	defer recConn.Close()

	listenAddr := recConn.LocalAddr().String()

	sender, err := NewSender(SenderConfig{
		TargetAddr:    listenAddr,
		Format:        DefaultAudioFormat,
		FrameDuration: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSender failed: %v", err)
	}
	defer sender.Close()

	receiver, err := NewReceiver(ReceiverConfig{
		Conn:        recConn,
		Format:      DefaultAudioFormat,
		JitterDelay: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewReceiver failed: %v", err)
	}
	defer receiver.Close()

	// Transmit 5 audio frames
	frameBytes := sender.FrameBytes()
	sampleChunk := make([]byte, frameBytes)
	for i := range sampleChunk {
		sampleChunk[i] = byte(i % 128)
	}

	for i := 0; i < 5; i++ {
		sampleChunk[0] = byte(i)
		if err := sender.Send(sampleChunk); err != nil {
			t.Fatalf("Send frame %d failed: %v", i, err)
		}
	}

	// Read and verify 5 packets from receiver
	for i := 0; i < 5; i++ {
		pkt, err := receiver.ReceivePacket(1 * time.Second)
		if err != nil {
			t.Fatalf("ReceivePacket %d failed: %v", i, err)
		}
		if pkt.Sequence != uint32(i) {
			t.Errorf("Packet %d Sequence = %d, want %d", i, pkt.Sequence, i)
		}
		if len(pkt.Data) != frameBytes {
			t.Fatalf("Packet %d Data len = %d, want %d", i, len(pkt.Data), frameBytes)
		}
		if pkt.Data[0] != byte(i) {
			t.Errorf("Packet %d Data[0] = %d, want %d", i, pkt.Data[0], i)
		}
	}

	pktsSent, bytesSent, _ := sender.Stats()
	if pktsSent != 5 {
		t.Errorf("Sender packetsSent = %d, want 5", pktsSent)
	}
	if bytesSent == 0 {
		t.Errorf("Sender bytesSent = 0")
	}
}

func TestAudioStreaming_EndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	recConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket failed: %v", err)
	}
	defer recConn.Close()

	listenAddr := recConn.LocalAddr().String()

	sender, err := NewSender(SenderConfig{
		TargetAddr:    listenAddr,
		Format:        DefaultAudioFormat,
		FrameDuration: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSender failed: %v", err)
	}
	defer sender.Close()

	receiver, err := NewReceiver(ReceiverConfig{
		Conn:        recConn,
		Format:      DefaultAudioFormat,
		JitterDelay: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewReceiver failed: %v", err)
	}
	defer receiver.Close()

	player := NewPlayer(DefaultAudioFormat)
	capturer := NewSilentCapturer(DefaultAudioFormat)

	// Run receiver stream in background
	streamErrCh := make(chan error, 1)
	go func() {
		streamErrCh <- receiver.Stream(ctx, player)
	}()

	// Give Windows audio driver time to initialize player
	time.Sleep(300 * time.Millisecond)

	// Run sender stream with capturer in background
	senderErrCh := make(chan error, 1)
	streamCtx, stopSender := context.WithCancel(ctx)
	go func() {
		senderErrCh <- sender.Stream(streamCtx, capturer)
	}()

	// Allow streaming for 500ms
	time.Sleep(500 * time.Millisecond)

	// Stop sender and wait for EOS
	stopSender()

	select {
	case err := <-senderErrCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Sender stream returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Sender stream did not terminate in time")
	}

	// Receiver should detect EOS or cancellation and complete
	cancel()

	select {
	case err := <-streamErrCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Receiver stream returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Receiver stream did not terminate in time")
	}

	// Check that packets were delivered and processed
	rec, played, _, _, _, _ := receiver.JitterBuffer().Stats()
	if rec == 0 {
		t.Errorf("Expected receiver to have processed packets, got 0")
	}
	if played == 0 {
		t.Errorf("Expected receiver to have played packets, got 0")
	}
}

func TestCapturerPlayer_Lifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	capturer := NewCapturer(DefaultAudioFormat)
	if capturer.Format() != DefaultAudioFormat {
		t.Errorf("Capturer format mismatch")
	}

	framesReceived := 0
	err := capturer.Start(ctx, func(pcm []byte) {
		if len(pcm) > 0 {
			framesReceived++
		}
	})
	if err != nil {
		t.Fatalf("Capturer Start failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	_ = capturer.Stop()

	player := NewPlayer(DefaultAudioFormat)
	if player.Format() != DefaultAudioFormat {
		t.Errorf("Player format mismatch")
	}

	if err := player.Start(ctx); err != nil {
		t.Fatalf("Player Start failed: %v", err)
	}

	dummyPCM := make([]byte, 1920)
	n, err := player.Write(dummyPCM)
	if err != nil {
		t.Fatalf("Player Write failed: %v", err)
	}
	if n != len(dummyPCM) {
		t.Errorf("Player Write returned %d, want %d", n, len(dummyPCM))
	}

	latency := player.BufferLatency()
	if latency > 100*time.Millisecond {
		t.Errorf("Player buffer latency too high: %v", latency)
	}

	if err := player.Stop(); err != nil {
		t.Fatalf("Player Stop failed: %v", err)
	}
}
