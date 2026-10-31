package audio

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// DefaultReadBufferSize is the socket read buffer size.
const DefaultReadBufferSize = 65535

// DefaultJitterDelay is the target playout buffering delay (30ms).
const DefaultJitterDelay = 30 * time.Millisecond

// DefaultMaxJitterPackets is the max packet capacity of the jitter buffer before dropping.
const DefaultMaxJitterPackets = 64

// packetHeap is a min-heap of AudioPacket ordered by Sequence (handling uint32 wrap-around).
type packetItem struct {
	packet   *AudioPacket
	recvTime time.Time
}

type packetHeap []packetItem

func (h packetHeap) Len() int { return len(h) }

// Less compares two sequence numbers with 32-bit modular arithmetic.
func (h packetHeap) Less(i, j int) bool {
	diff := int32(h[i].packet.Sequence - h[j].packet.Sequence)
	return diff < 0
}

func (h packetHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *packetHeap) Push(x any) {
	*h = append(*h, x.(packetItem))
}

func (h *packetHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[0 : n-1]
	return item
}

// JitterBuffer reorders, deduplicates, and smoothes out UDP audio jitter.
type JitterBuffer struct {
	mu           sync.Mutex
	format       AudioFormat
	targetDelay  time.Duration
	maxPackets   int
	heap         packetHeap
	seenSeqs     map[uint32]struct{}
	expectedSeq  uint32
	initialized  bool
	buffering    bool
	silenceFrame []byte

	packetsReceived uint64
	packetsPlayed   uint64
	packetsLost     uint64
	packetsDropped  uint64 // late or duplicate
	underruns       uint64
	overruns        uint64
}

// NewJitterBuffer creates an adaptive audio jitter buffer.
func NewJitterBuffer(fmt AudioFormat, targetDelay time.Duration, maxPackets int) *JitterBuffer {
	if targetDelay <= 0 {
		targetDelay = DefaultJitterDelay
	}
	if maxPackets <= 0 {
		maxPackets = DefaultMaxJitterPackets
	}

	frameBytes := fmt.DurationToBytes(DefaultFrameDuration)
	if frameBytes <= 0 {
		frameBytes = fmt.FrameSize() * 480
	}

	jb := &JitterBuffer{
		format:       fmt,
		targetDelay:  targetDelay,
		maxPackets:   maxPackets,
		heap:         make(packetHeap, 0, maxPackets),
		seenSeqs:     make(map[uint32]struct{}),
		buffering:    true,
		silenceFrame: make([]byte, frameBytes),
	}
	heap.Init(&jb.heap)
	return jb
}

// Push adds an incoming AudioPacket into the jitter buffer.
func (jb *JitterBuffer) Push(pkt *AudioPacket) {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	jb.packetsReceived++

	// First packet initializes the sequence tracking
	if !jb.initialized {
		jb.expectedSeq = pkt.Sequence
		jb.initialized = true
		jb.buffering = true
	}

	seqDiff := int32(pkt.Sequence - jb.expectedSeq)

	// Check if packet is late (older than expectedSeq)
	if seqDiff < 0 {
		jb.packetsDropped++
		return
	}

	// Check for duplicates
	if _, exists := jb.seenSeqs[pkt.Sequence]; exists {
		jb.packetsDropped++
		return
	}

	// Overrun guard: if buffer is overflowing, drop oldest or fast-forward
	if jb.heap.Len() >= jb.maxPackets {
		jb.overruns++
		// Fast-forward expected sequence to avoid infinite latency build up
		oldest := heap.Pop(&jb.heap).(packetItem)
		delete(jb.seenSeqs, oldest.packet.Sequence)
		jb.expectedSeq = oldest.packet.Sequence
	}

	jb.seenSeqs[pkt.Sequence] = struct{}{}
	heap.Push(&jb.heap, packetItem{
		packet:   pkt,
		recvTime: time.Now(),
	})

	// If packet is EOS, unbuffer immediately so stream can terminate cleanly
	if pkt.Flags&FlagEndOfStream != 0 {
		jb.buffering = false
		return
	}

	// Check if we have buffered enough packets to start playout
	if jb.buffering {
		bufferedDur := time.Duration(jb.heap.Len()) * DefaultFrameDuration
		if bufferedDur >= jb.targetDelay || jb.heap.Len() >= 3 {
			jb.buffering = false
		}
	}
}

// Pop retrieves the next in-order PCM frame. If a packet is lost, it outputs concealment silence.
func (jb *JitterBuffer) Pop() ([]byte, bool, error) {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	if !jb.initialized {
		return nil, false, nil
	}

	if jb.heap.Len() == 0 {
		jb.underruns++
		jb.buffering = true
		return nil, false, nil
	}

	// If next item in heap is EOS, unbuffer immediately
	if jb.heap[0].packet.Flags&FlagEndOfStream != 0 {
		jb.buffering = false
	}

	if jb.buffering {
		return nil, false, nil
	}

	minSeq := jb.heap[0].packet.Sequence
	seqDiff := int32(minSeq - jb.expectedSeq)

	if seqDiff == 0 {
		// Exact next packet
		item := heap.Pop(&jb.heap).(packetItem)
		delete(jb.seenSeqs, item.packet.Sequence)
		jb.expectedSeq++
		jb.packetsPlayed++
		isEOS := (item.packet.Flags & FlagEndOfStream) != 0
		return item.packet.Data, isEOS, nil
	}

	if seqDiff < 0 {
		// Stale packet that was somehow retained, discard and retry
		item := heap.Pop(&jb.heap).(packetItem)
		delete(jb.seenSeqs, item.packet.Sequence)
		jb.packetsDropped++
		return nil, false, nil
	}

	// seqDiff > 0: Gap detected. Packet jb.expectedSeq is missing!
	// If we have enough queued items ahead, declare packet lost and conceal with silence.
	if jb.heap.Len() >= 2 || seqDiff > 5 {
		jb.packetsLost++
		jb.expectedSeq++
		silence := make([]byte, len(jb.silenceFrame))
		return silence, false, nil
	}

	// Still waiting for the missing packet to arrive
	return nil, false, nil
}

// Stats returns runtime performance metrics of the jitter buffer.
func (jb *JitterBuffer) Stats() (received, played, lost, dropped, underruns, overruns uint64) {
	jb.mu.Lock()
	defer jb.mu.Unlock()
	return jb.packetsReceived, jb.packetsPlayed, jb.packetsLost, jb.packetsDropped, jb.underruns, jb.overruns
}

// Receiver listens for UDP audio broadcast and outputs smoothed audio streams to a Player.
type Receiver struct {
	mu           sync.Mutex
	conn         net.PacketConn
	ownsConn     bool
	format       AudioFormat
	jitterBuffer *JitterBuffer
	closed       bool
	stopCh       chan struct{}
}

// NewReceiver creates an audio receiver bound to a local or multicast address.
func NewReceiver(cfg ReceiverConfig) (*Receiver, error) {
	fmtSpec := cfg.Format
	if err := fmtSpec.Validate(); err != nil {
		fmtSpec = DefaultAudioFormat
	}

	listenAddr := cfg.ListenAddr
	if listenAddr == "" {
		listenAddr = DefaultAudioMulticastAddr
	}

	conn := cfg.Conn
	ownsConn := false

	if conn == nil {
		udpAddr, err := net.ResolveUDPAddr("udp4", listenAddr)
		if err != nil {
			return nil, fmt.Errorf("audio receiver: resolve listen address failed: %w", err)
		}

		if udpAddr.IP.IsMulticast() {
			mcastConn, err := net.ListenMulticastUDP("udp4", nil, udpAddr)
			if err != nil {
				return nil, fmt.Errorf("audio receiver: listen multicast failed: %w", err)
			}
			conn = mcastConn
		} else {
			uConn, err := net.ListenPacket("udp4", listenAddr)
			if err != nil {
				return nil, fmt.Errorf("audio receiver: listen packet failed: %w", err)
			}
			conn = uConn
		}
		ownsConn = true
	}

	jb := NewJitterBuffer(fmtSpec, cfg.JitterDelay, cfg.MaxJitterSize)

	return &Receiver{
		conn:         conn,
		ownsConn:     ownsConn,
		format:       fmtSpec,
		jitterBuffer: jb,
		stopCh:       make(chan struct{}),
	}, nil
}

// Stream listens for incoming packets and delivers audio directly to the provided Player.
// It blocks until ctx is cancelled or the stream ends.
func (r *Receiver) Stream(ctx context.Context, player Player) error {
	if err := player.Start(ctx); err != nil {
		return fmt.Errorf("audio receiver: start player failed: %w", err)
	}
	defer player.Stop()

	// Watcher to unblock ReadFrom when ctx cancelled or receiver stopped
	cancelWatcher := make(chan struct{})
	defer close(cancelWatcher)
	go func() {
		select {
		case <-cancelWatcher:
			return
		case <-r.stopCh:
			_ = r.conn.SetReadDeadline(time.Now())
		case <-ctx.Done():
			_ = r.conn.SetReadDeadline(time.Now())
		}
	}()

	// Launch UDP packet ingest loop
	ingestErrCh := make(chan error, 1)
	go func() {
		buf := make([]byte, DefaultReadBufferSize)
		for {
			select {
			case <-r.stopCh:
				return
			case <-ctx.Done():
				return
			default:
			}

			_ = r.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, _, err := r.conn.ReadFrom(buf)
			if err != nil {
				var netErr net.Error
				if errors.As(err, &netErr) && netErr.Timeout() {
					continue
				}
				r.mu.Lock()
				closed := r.closed
				r.mu.Unlock()
				if closed {
					return
				}
				select {
				case ingestErrCh <- fmt.Errorf("ingest ReadFrom failed: %w", err):
				default:
				}
				return
			}

			pkt := &AudioPacket{}
			if err := pkt.UnmarshalBinary(buf[:n]); err != nil {
				continue
			}

			r.jitterBuffer.Push(pkt)
		}
	}()

	// Playout loop: ticks at frame duration to smoothly drain jitter buffer into player
	ticker := time.NewTicker(DefaultFrameDuration)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case err := <-ingestErrCh:
			return err
		case <-ticker.C:
			for {
				pcm, isEOS, err := r.jitterBuffer.Pop()
				if err != nil {
					return err
				}
				if isEOS {
					return nil
				}
				if pcm == nil {
					break
				}
				if len(pcm) > 0 {
					if _, err := player.Write(pcm); err != nil {
						return fmt.Errorf("audio receiver: player write failed: %w", err)
					}
				}
			}
		}
	}
}

// ReceivePacket reads a single validated AudioPacket from the network.
func (r *Receiver) ReceivePacket(timeout time.Duration) (*AudioPacket, error) {
	buf := make([]byte, DefaultReadBufferSize)
	if timeout > 0 {
		_ = r.conn.SetReadDeadline(time.Now().Add(timeout))
	} else {
		_ = r.conn.SetReadDeadline(time.Time{})
	}

	n, _, err := r.conn.ReadFrom(buf)
	if err != nil {
		return nil, err
	}

	pkt := &AudioPacket{}
	if err := pkt.UnmarshalBinary(buf[:n]); err != nil {
		return nil, err
	}
	return pkt, nil
}

// JitterBuffer returns the underlying JitterBuffer instance.
func (r *Receiver) JitterBuffer() *JitterBuffer {
	return r.jitterBuffer
}

// Format returns the configured audio format.
func (r *Receiver) Format() AudioFormat {
	return r.format
}

// Close terminates network listening and cleans up resources.
func (r *Receiver) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true
	close(r.stopCh)

	if r.conn != nil {
		_ = r.conn.SetReadDeadline(time.Now())
	}

	var err error
	if r.ownsConn && r.conn != nil {
		err = r.conn.Close()
	}
	return err
}
