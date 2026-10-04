package quic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"localmesh/internal/protocol"
)

var (
	ErrNotConfigured = errors.New("quic transport is not configured")
	ErrPeerMismatch  = errors.New("quic peer identity mismatch")
)

type Stream interface {
	io.ReadWriteCloser
	SetDeadline(time.Time) error
}
type StreamOpener interface {
	Open(context.Context) (Stream, error)
}

type Client struct {
	Opener        StreamOpener
	PeerDeviceID  string
	MaxConcurrent int
	mu            sync.Mutex
	inflight      int
}

func (c *Client) Send(ctx context.Context, envelope protocol.Envelope) error {
	if c.Opener == nil {
		return ErrNotConfigured
	}
	if envelope.SenderID == "" || (c.PeerDeviceID != "" && envelope.SenderID != c.PeerDeviceID) {
		return ErrPeerMismatch
	}
	if err := envelope.Validate(time.Now()); err != nil {
		return fmt.Errorf("validate envelope: %w", err)
	}
	if !c.acquire() {
		return errors.New("quic stream limit reached")
	}
	defer c.release()
	stream, err := c.Opener.Open(ctx)
	if err != nil {
		return fmt.Errorf("open quic stream: %w", err)
	}
	defer stream.Close()
	if !envelope.Deadline.IsZero() {
		_ = stream.SetDeadline(envelope.Deadline)
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	if _, err := stream.Write(data); err != nil {
		return fmt.Errorf("write quic envelope: %w", err)
	}
	return nil
}

func (c *Client) acquire() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.MaxConcurrent > 0 && c.inflight >= c.MaxConcurrent {
		return false
	}
	c.inflight++
	return true
}
func (c *Client) release() { c.mu.Lock(); c.inflight--; c.mu.Unlock() }

var _ interface {
	Send(context.Context, protocol.Envelope) error
} = (*Client)(nil)
