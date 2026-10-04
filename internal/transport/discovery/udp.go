package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	domain "localmesh/internal/domain/discovery"
	"localmesh/internal/domain/identity"
)

const ProtocolVersion = 1

var ErrPacketTooLarge = errors.New("discovery packet too large")

type Announcement struct {
	Version      int      `json:"version"`
	DeviceID     string   `json:"device_id"`
	Endpoint     string   `json:"endpoint,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	TTLSeconds   int      `json:"ttl_seconds"`
}

type Observer interface{ Observe(domain.Observation) }

type Server struct {
	Conn        *net.UDPConn
	Observer    Observer
	MaxPacket   int
	MinInterval time.Duration
	mu          sync.Mutex
	last        map[string]time.Time
}

func Listen(addr *net.UDPAddr, observer Observer) (*Server, error) {
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen discovery udp: %w", err)
	}
	return &Server{Conn: conn, Observer: observer, MaxPacket: 4096, MinInterval: 100 * time.Millisecond, last: make(map[string]time.Time)}, nil
}

func (s *Server) Close() error {
	if s.Conn == nil {
		return nil
	}
	return s.Conn.Close()
}

func (s *Server) Serve(ctx context.Context) error {
	if s.Conn == nil || s.Observer == nil {
		return errors.New("discovery server is not configured")
	}
	maxPacket := s.MaxPacket
	if maxPacket <= 0 {
		maxPacket = 4096
	}
	buffer := make([]byte, maxPacket+1)
	for {
		_ = s.Conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		n, remote, err := s.Conn.ReadFromUDP(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return fmt.Errorf("read discovery packet: %w", err)
		}
		if n > maxPacket {
			continue
		}
		var announcement Announcement
		if err := json.Unmarshal(buffer[:n], &announcement); err != nil || announcement.Version != ProtocolVersion {
			continue
		}
		deviceID, err := identity.NewDeviceID(announcement.DeviceID)
		if err != nil {
			continue
		}
		key := string(deviceID)
		now := time.Now()
		s.mu.Lock()
		last := s.last[key]
		if !last.IsZero() && now.Sub(last) < s.MinInterval {
			s.mu.Unlock()
			continue
		}
		s.last[key] = now
		s.mu.Unlock()
		address, err := netip.ParseAddrPort(remote.String())
		if err != nil {
			continue
		}
		s.Observer.Observe(domain.Observation{DeviceID: deviceID, Address: address, Method: domain.MethodBroadcast, SeenAt: now, TLSName: announcement.Endpoint, Reachable: true})
	}
}
