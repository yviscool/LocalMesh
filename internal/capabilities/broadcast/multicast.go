package broadcast

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	DefaultMulticastAddr = "239.255.42.99:8899"
	DefaultUDPBufferSize = 65535
	ReadTimeoutInterval  = 100 * time.Millisecond
)

var (
	ErrSenderClosed   = errors.New("multicast sender is closed")
	ErrReceiverClosed = errors.New("multicast receiver is closed")
	ErrInvalidAddress = errors.New("invalid multicast/broadcast address")
	ErrNoConnProvided = errors.New("no packet connection or listen address specified")
)

// MulticastSenderConfig configures a MulticastSender.
type MulticastSenderConfig struct {
	TargetAddr string         // e.g. "239.255.42.99:8899" or "127.0.0.1:8899"
	Conn       net.PacketConn // Injected PacketConn for testing or non-standard interfaces
	ChunkSize  int            // Max payload size per chunk (default: 1200)
	FEC        FECConfig      // Optional Forward Error Correction configuration
}

// MulticastSender distributes ScreenFrames and PacketChunks over UDP multicast or broadcast.
type MulticastSender struct {
	mu         sync.Mutex
	conn       net.PacketConn
	targetAddr net.Addr
	chunkSize  int
	fecConfig  FECConfig
	ownsConn   bool
	closed     bool
}

// NewMulticastSender constructs a MulticastSender.
// If cfg.Conn is provided, it is used directly without binding to physical network adapters.
func NewMulticastSender(cfg MulticastSenderConfig) (*MulticastSender, error) {
	chunkSize := cfg.ChunkSize
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	if chunkSize > MaxChunkPayload {
		return nil, ErrChunkTooLarge
	}

	targetStr := cfg.TargetAddr
	if targetStr == "" {
		targetStr = DefaultMulticastAddr
	}

	dstAddr, err := net.ResolveUDPAddr("udp4", targetStr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidAddress, err)
	}

	conn := cfg.Conn
	ownsConn := false
	if conn == nil {
		var err error
		conn, err = net.ListenPacket("udp4", ":0")
		if err != nil {
			return nil, fmt.Errorf("failed to bind UDP sender socket: %w", err)
		}
		ownsConn = true
	}

	return &MulticastSender{
		conn:       conn,
		targetAddr: dstAddr,
		chunkSize:  chunkSize,
		fecConfig:  cfg.FEC,
		ownsConn:   ownsConn,
	}, nil
}

// SendFrame chunks and transmits a complete ScreenFrame over UDP.
// If FEC is enabled, redundant parity chunks are generated and transmitted alongside data chunks.
func (s *MulticastSender) SendFrame(ctx context.Context, frame ScreenFrame) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrSenderClosed
	}
	conn := s.conn
	targetAddr := s.targetAddr
	chunkSize := s.chunkSize
	fec := s.fecConfig
	s.mu.Unlock()

	chunks, err := ChunkFrame(frame, chunkSize)
	if err != nil {
		return fmt.Errorf("chunk frame failed: %w", err)
	}

	allChunks := chunks
	if fec.Enabled && fec.DataShards > 0 && fec.ParityShards > 0 {
		parityChunks, err := BuildFECParityChunks(chunks, fec.DataShards, fec.ParityShards)
		if err != nil {
			return fmt.Errorf("build fec parity chunks failed: %w", err)
		}
		if len(parityChunks) > 0 {
			allChunks = make([]PacketChunk, 0, len(chunks)+len(parityChunks))
			allChunks = append(allChunks, chunks...)
			allChunks = append(allChunks, parityChunks...)
		}
	}

	for _, chunk := range allChunks {
		if err := ctx.Err(); err != nil {
			return err
		}

		packet, err := chunk.MarshalBinary()
		if err != nil {
			return fmt.Errorf("marshal chunk failed: %w", err)
		}

		if _, err := conn.WriteTo(packet, targetAddr); err != nil {
			return fmt.Errorf("udp write failed: %w", err)
		}
	}

	return nil
}

// FECConfig returns the sender's current FEC settings.
func (s *MulticastSender) FECConfig() FECConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fecConfig
}

// SetFECConfig updates the FEC configuration on the sender.
func (s *MulticastSender) SetFECConfig(cfg FECConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fecConfig = cfg
}

// SendChunk sends an individual PacketChunk over UDP.
func (s *MulticastSender) SendChunk(chunk PacketChunk) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrSenderClosed
	}
	conn := s.conn
	targetAddr := s.targetAddr
	s.mu.Unlock()

	packet, err := chunk.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal chunk failed: %w", err)
	}

	if _, err := conn.WriteTo(packet, targetAddr); err != nil {
		return fmt.Errorf("udp write failed: %w", err)
	}

	return nil
}

// TargetAddr returns the destination address of the sender.
func (s *MulticastSender) TargetAddr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.targetAddr
}

// LocalAddr returns the local address bound by the sender's packet conn.
func (s *MulticastSender) LocalAddr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		return s.conn.LocalAddr()
	}
	return nil
}

// Close closes the MulticastSender and releases resources if owned.
func (s *MulticastSender) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.ownsConn && s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

// MulticastReceiverConfig configures a MulticastReceiver.
type MulticastReceiverConfig struct {
	ListenAddr         string         // e.g. "239.255.42.99:8899" or ":8899"
	Conn               net.PacketConn // Injected PacketConn for testing or detached execution
	BufferSize         int            // Datagram read buffer size (default: 65535)
	ReassemblerTimeout time.Duration  // Frame reassembly timeout (default: 1s)
}

// MulticastReceiver listens for UDP datagrams and reassembles them into ScreenFrames.
type MulticastReceiver struct {
	mu          sync.Mutex
	conn        net.PacketConn
	reassembler *FrameReassembler
	bufferSize  int
	ownsConn    bool
	closed      bool
	closeCh     chan struct{}
}

// NewMulticastReceiver constructs a MulticastReceiver.
// Supports injected net.PacketConn for unit testing and detached non-physical network execution.
func NewMulticastReceiver(cfg MulticastReceiverConfig) (*MulticastReceiver, error) {
	bufSize := cfg.BufferSize
	if bufSize <= 0 {
		bufSize = DefaultUDPBufferSize
	}

	reassembler := NewFrameReassembler(cfg.ReassemblerTimeout)

	conn := cfg.Conn
	ownsConn := false

	if conn == nil {
		listenStr := cfg.ListenAddr
		if listenStr == "" {
			listenStr = DefaultMulticastAddr
		}

		udpAddr, err := net.ResolveUDPAddr("udp4", listenStr)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidAddress, err)
		}

		if udpAddr.IP.IsMulticast() {
			mConn, err := net.ListenMulticastUDP("udp4", nil, udpAddr)
			if err != nil {
				// Fallback to regular packet conn if interface multicast join fails
				pConn, err2 := net.ListenPacket("udp4", listenStr)
				if err2 != nil {
					return nil, fmt.Errorf("failed to bind multicast/udp receiver: %w (fallback: %v)", err, err2)
				}
				conn = pConn
			} else {
				conn = mConn
			}
		} else {
			pConn, err := net.ListenPacket("udp4", listenStr)
			if err != nil {
				return nil, fmt.Errorf("failed to bind UDP receiver: %w", err)
			}
			conn = pConn
		}
		ownsConn = true
	}

	return &MulticastReceiver{
		conn:        conn,
		reassembler: reassembler,
		bufferSize:  bufSize,
		ownsConn:    ownsConn,
		closeCh:     make(chan struct{}),
	}, nil
}

// ReceiveChunk reads a single PacketChunk datagram from the network.
func (r *MulticastReceiver) ReceiveChunk() (PacketChunk, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return PacketChunk{}, ErrReceiverClosed
	}
	conn := r.conn
	bufSize := r.bufferSize
	r.mu.Unlock()

	buf := make([]byte, bufSize)
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		return PacketChunk{}, err
	}

	var chunk PacketChunk
	if err := chunk.UnmarshalBinary(buf[:n]); err != nil {
		return PacketChunk{}, fmt.Errorf("unmarshal chunk failed: %w", err)
	}

	return chunk, nil
}

// ReceiveFrame reads datagrams from the network until a complete ScreenFrame is reassembled
// or the context is cancelled.
func (r *MulticastReceiver) ReceiveFrame(ctx context.Context) (*ScreenFrame, error) {
	buf := make([]byte, r.bufferSize)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-r.closeCh:
			return nil, ErrReceiverClosed
		default:
		}

		// Set a short read deadline so we periodically check context cancellation
		_ = r.conn.SetReadDeadline(time.Now().Add(ReadTimeoutInterval))

		n, _, err := r.conn.ReadFrom(buf)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				// Read deadline exceeded: loop back to check context
				continue
			}
			select {
			case <-r.closeCh:
				return nil, ErrReceiverClosed
			default:
				return nil, fmt.Errorf("udp read failed: %w", err)
			}
		}

		var chunk PacketChunk
		if err := chunk.UnmarshalBinary(buf[:n]); err != nil {
			// Discard malformed packet
			continue
		}

		frame, err := r.reassembler.AddChunk(chunk)
		if err != nil {
			// CRC mismatch or invalid chunk: discard and continue
			continue
		}

		if frame != nil {
			return frame, nil
		}
	}
}

// Start launches a background worker that continuously reassembles incoming frames.
func (r *MulticastReceiver) Start(ctx context.Context) (<-chan *ScreenFrame, <-chan error) {
	frameCh := make(chan *ScreenFrame, 16)
	errCh := make(chan error, 1)

	go func() {
		defer close(frameCh)
		defer close(errCh)

		for {
			frame, err := r.ReceiveFrame(ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, ErrReceiverClosed) {
					return
				}
				select {
				case errCh <- err:
				default:
				}
				return
			}

			select {
			case <-ctx.Done():
				return
			case <-r.closeCh:
				return
			case frameCh <- frame:
			}
		}
	}()

	return frameCh, errCh
}

// Reassembler returns the internal FrameReassembler instance.
func (r *MulticastReceiver) Reassembler() *FrameReassembler {
	return r.reassembler
}

// LocalAddr returns the local address bound by the receiver.
func (r *MulticastReceiver) LocalAddr() net.Addr {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conn != nil {
		return r.conn.LocalAddr()
	}
	return nil
}

// Close closes the MulticastReceiver and releases the connection if owned.
func (r *MulticastReceiver) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	close(r.closeCh)

	// Interrupt any pending ReadFrom
	if r.conn != nil {
		_ = r.conn.SetReadDeadline(time.Now())
		if r.ownsConn {
			return r.conn.Close()
		}
	}
	return nil
}
