package audio

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	// DefaultAudioMulticastAddr is the standard multicast target for classroom audio streaming.
	DefaultAudioMulticastAddr = "239.255.42.99:8900"
	// DefaultFrameDuration is the standard 10ms low-latency frame duration.
	DefaultFrameDuration = 10 * time.Millisecond
	// DefaultMaxPacketSize is the maximum safe UDP MTU payload.
	DefaultMaxPacketSize = 2048
)

// Sender encodes and distributes live audio streams over UDP multicast or unicast.
type Sender struct {
	mu            sync.Mutex
	conn          net.PacketConn
	targetAddr    net.Addr
	format        AudioFormat
	frameDuration time.Duration
	frameBytes    int
	maxPacketSize int
	ownsConn      bool
	closed        bool
	buffer        []byte

	seq         uint32
	startTime   time.Time
	samplesSent uint64
	bytesSent   uint64
	packetsSent uint64
}

// NewSender constructs a new audio Sender.
func NewSender(cfg SenderConfig) (*Sender, error) {
	fmtSpec := cfg.Format
	if err := fmtSpec.Validate(); err != nil {
		fmtSpec = DefaultAudioFormat
	}

	frameDur := cfg.FrameDuration
	if frameDur <= 0 {
		frameDur = DefaultFrameDuration
	}

	maxPkt := cfg.MaxPacketSize
	if maxPkt <= 0 {
		maxPkt = DefaultMaxPacketSize
	}

	target := cfg.TargetAddr
	if target == "" {
		target = DefaultAudioMulticastAddr
	}

	dstAddr, err := net.ResolveUDPAddr("udp4", target)
	if err != nil {
		return nil, fmt.Errorf("audio sender: resolve target address failed: %w", err)
	}

	conn := cfg.Conn
	ownsConn := false
	if conn == nil {
		c, err := net.ListenPacket("udp4", ":0")
		if err != nil {
			return nil, fmt.Errorf("audio sender: failed to bind UDP socket: %w", err)
		}
		conn = c
		ownsConn = true
	}

	// Calculate payload bytes per frame
	frameBytes := fmtSpec.DurationToBytes(frameDur)
	if rem := frameBytes % fmtSpec.FrameSize(); rem != 0 {
		frameBytes -= rem
	}

	maxPayload := maxPkt - AudioPacketHeaderSize
	if frameBytes > maxPayload {
		frameBytes = (maxPayload / fmtSpec.FrameSize()) * fmtSpec.FrameSize()
	}
	if frameBytes <= 0 {
		frameBytes = fmtSpec.FrameSize()
	}

	return &Sender{
		conn:          conn,
		targetAddr:    dstAddr,
		format:        fmtSpec,
		frameDuration: frameDur,
		frameBytes:    frameBytes,
		maxPacketSize: maxPkt,
		ownsConn:      ownsConn,
		startTime:     time.Now(),
		buffer:        make([]byte, 0, frameBytes*4),
	}, nil
}

// FrameBytes returns the exact byte size of one PCM audio packet frame.
func (s *Sender) FrameBytes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.frameBytes
}

// Send consumes raw PCM bytes, chunks them into uniform frames, and transmits them.
func (s *Sender) Send(pcm []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrSenderClosed
	}

	s.buffer = append(s.buffer, pcm...)
	for len(s.buffer) >= s.frameBytes {
		chunk := s.buffer[:s.frameBytes]
		if err := s.sendChunkLocked(chunk, FlagNone); err != nil {
			return err
		}
		s.buffer = s.buffer[s.frameBytes:]
	}
	return nil
}

// Flush forces any remaining partial PCM buffer to be sent.
func (s *Sender) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrSenderClosed
	}

	if len(s.buffer) > 0 {
		rem := len(s.buffer) % s.format.FrameSize()
		if rem != 0 {
			pad := s.format.FrameSize() - rem
			s.buffer = append(s.buffer, make([]byte, pad)...)
		}
		err := s.sendChunkLocked(s.buffer, FlagNone)
		s.buffer = s.buffer[:0]
		return err
	}
	return nil
}

// sendChunkLocked creates an AudioPacket and writes it to the socket. Caller must hold s.mu.
func (s *Sender) sendChunkLocked(data []byte, flags uint8) error {
	seq := s.seq
	s.seq++

	ts := uint64(time.Since(s.startTime).Microseconds())
	pkt := NewAudioPacket(seq, ts, s.format, flags, data)

	wireBytes, err := pkt.MarshalBinary()
	if err != nil {
		return fmt.Errorf("audio sender: marshal packet failed: %w", err)
	}

	_, err = s.conn.WriteTo(wireBytes, s.targetAddr)
	if err != nil {
		return fmt.Errorf("audio sender: socket write failed: %w", err)
	}

	s.packetsSent++
	s.bytesSent += uint64(len(wireBytes))
	samples := len(data) / s.format.FrameSize()
	s.samplesSent += uint64(samples)
	return nil
}

// Stream captures audio continuously from a Capturer and streams it until ctx is cancelled.
func (s *Sender) Stream(ctx context.Context, cap Capturer) error {
	errCh := make(chan error, 1)

	handler := func(pcm []byte) {
		if err := s.Send(pcm); err != nil {
			select {
			case errCh <- err:
			default:
			}
		}
	}

	if err := cap.Start(ctx, handler); err != nil {
		return fmt.Errorf("audio sender: start capturer failed: %w", err)
	}
	defer cap.Stop()

	select {
	case <-ctx.Done():
		s.mu.Lock()
		if !s.closed {
			// Flush any residual buffer and send EOS
			if len(s.buffer) > 0 {
				rem := len(s.buffer) % s.format.FrameSize()
				if rem != 0 {
					pad := s.format.FrameSize() - rem
					s.buffer = append(s.buffer, make([]byte, pad)...)
				}
				_ = s.sendChunkLocked(s.buffer, FlagNone)
				s.buffer = s.buffer[:0]
			}
			_ = s.sendChunkLocked(nil, FlagEndOfStream)
		}
		s.mu.Unlock()
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// Stats returns runtime statistics of the audio sender.
func (s *Sender) Stats() (packets uint64, bytes uint64, samples uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.packetsSent, s.bytesSent, s.samplesSent
}

// Format returns the audio format.
func (s *Sender) Format() AudioFormat {
	return s.format
}

// NextSequence returns the current packet sequence counter.
func (s *Sender) NextSequence() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seq
}

// Close gracefully terminates the sender.
func (s *Sender) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	// Send an end of stream indicator packet
	_ = s.sendChunkLocked(nil, FlagEndOfStream)

	var err error
	if s.ownsConn && s.conn != nil {
		err = s.conn.Close()
	}
	return err
}
