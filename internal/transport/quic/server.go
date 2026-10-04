package quic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"localmesh/internal/protocol"
)

var (
	ErrServerNotConfigured = errors.New("quic server is not configured")
	ErrPeerIdentity        = errors.New("quic peer identity is missing")
)

type StreamAcceptor interface {
	Accept(context.Context) (Stream, error)
}
type Handler func(context.Context, protocol.Envelope) error

type Server struct {
	Acceptor     StreamAcceptor
	PeerDeviceID string
	Handler      Handler
	MaxMessage   int
}

func (s *Server) Serve(ctx context.Context) error {
	if s.Acceptor == nil || s.Handler == nil || s.PeerDeviceID == "" {
		return ErrServerNotConfigured
	}
	max := s.MaxMessage
	if max <= 0 {
		max = protocol.MaxMessageBytes
	}
	for {
		stream, err := s.Acceptor.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("accept quic stream: %w", err)
		}
		if err := s.handle(ctx, stream, max); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func (s *Server) handle(ctx context.Context, stream Stream, max int) error {
	defer stream.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = stream.SetDeadline(deadline)
	}
	data, err := io.ReadAll(io.LimitReader(stream, int64(max)+1))
	if err != nil {
		return err
	}
	if len(data) > max {
		return protocol.ErrMessageTooLarge
	}
	var envelope protocol.Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("decode quic envelope: %w", err)
	}
	if envelope.SenderID != s.PeerDeviceID {
		return ErrPeerMismatch
	}
	if err := envelope.Validate(time.Now()); err != nil {
		return fmt.Errorf("validate quic envelope: %w", err)
	}
	return s.Handler(ctx, envelope)
}
