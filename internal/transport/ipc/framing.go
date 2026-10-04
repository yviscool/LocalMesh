package ipc

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const DefaultMaxMessage = 1 << 20

var (
	ErrMessageTooLarge  = errors.New("ipc message too large")
	ErrDuplicateRequest = errors.New("duplicate ipc request")
)

type Request struct {
	ID          string         `json:"request_id"`
	SessionID   string         `json:"session_id"`
	ClassroomID string         `json:"classroom_id"`
	Capability  string         `json:"capability"`
	Action      string         `json:"action"`
	TargetKind  string         `json:"target_kind"`
	TargetID    string         `json:"target_id"`
	Payload     map[string]any `json:"payload,omitempty"`
}

type Response struct {
	ID      string         `json:"request_id"`
	OK      bool           `json:"ok"`
	Code    string         `json:"code,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
}

type Handler interface {
	Handle(context.Context, Request) Response
}

type Authorizer interface {
	Allow(context.Context, string, string, string, string, string) error
}

// CapabilityHandler makes the Service boundary repeat authorization checks even
// when a request originated from a local user agent.
type CapabilityHandler struct {
	Authorizer Authorizer
	Next       Handler
}

func (h CapabilityHandler) Handle(ctx context.Context, request Request) Response {
	if h.Authorizer == nil || h.Next == nil {
		return Response{ID: request.ID, Code: "ipc_not_configured"}
	}
	if err := h.Authorizer.Allow(ctx, request.SessionID, request.ClassroomID, request.Capability, request.TargetKind, request.TargetID); err != nil {
		return Response{ID: request.ID, Code: "unauthorized"}
	}
	return h.Next.Handle(ctx, request)
}

func Write(w io.Writer, value any, max int) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode ipc message: %w", err)
	}
	if max <= 0 {
		max = DefaultMaxMessage
	}
	if len(data) > max {
		return ErrMessageTooLarge
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	if _, err := w.Write(header[:]); err != nil {
		return fmt.Errorf("write ipc header: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write ipc payload: %w", err)
	}
	return nil
}

func Read(r io.Reader, target any, max int) error {
	if max <= 0 {
		max = DefaultMaxMessage
	}
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return err
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 || uint64(length) > uint64(max) {
		return ErrMessageTooLarge
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return fmt.Errorf("read ipc payload: %w", err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode ipc message: %w", err)
	}
	return nil
}

func Serve(ctx context.Context, rw io.ReadWriter, handler Handler, max int) error {
	if rw == nil || handler == nil {
		return errors.New("ipc server is not configured")
	}
	seen := make(map[string]struct{})
	for {
		var request Request
		if err := Read(rw, &request, max); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		if request.ID == "" {
			return errors.New("ipc request id is required")
		}
		if _, exists := seen[request.ID]; exists {
			if err := Write(rw, Response{ID: request.ID, Code: ErrDuplicateRequest.Error()}, max); err != nil {
				return err
			}
			continue
		}
		seen[request.ID] = struct{}{}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := Write(rw, handler.Handle(ctx, request), max); err != nil {
			return err
		}
	}
}
