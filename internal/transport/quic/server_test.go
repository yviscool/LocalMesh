package quic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"localmesh/internal/protocol"
)

type acceptOnce struct {
	stream Stream
	used   bool
}

func (a *acceptOnce) Accept(context.Context) (Stream, error) {
	if a.used {
		return nil, io.EOF
	}
	a.used = true
	return a.stream, nil
}

type readStream struct {
	io.Reader
	strings.Builder
}

func (s *readStream) Write(p []byte) (int, error) { return s.Builder.Write(p) }
func (s *readStream) Close() error                { return nil }
func (s *readStream) SetDeadline(time.Time) error { return nil }

func TestServerValidatesPeerAndDispatchesEnvelope(t *testing.T) {
	now := time.Now()
	env := protocol.Envelope{ProtocolVersion: protocol.CurrentVersion, MessageType: protocol.MessageCommandRequest, MessageID: "m1", RequestID: "r1", SessionID: "s1", ClassroomID: "c1", SenderID: "device-1", SentAt: now, Deadline: now.Add(time.Minute), IdempotencyKey: "i1"}
	data, _ := json.Marshal(env)
	stream := &readStream{Reader: strings.NewReader(string(data))}
	called := false
	server := &Server{Acceptor: &acceptOnce{stream: stream}, PeerDeviceID: "device-1", Handler: func(context.Context, protocol.Envelope) error { called = true; return nil }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := server.Serve(ctx)
	if err == nil {
		t.Fatal("Serve should stop after acceptor EOF")
	}
	if !called {
		t.Fatal("handler not called")
	}
}

func TestServerRejectsPeerMismatch(t *testing.T) {
	now := time.Now()
	env := protocol.Envelope{ProtocolVersion: protocol.CurrentVersion, MessageType: protocol.MessageCommandRequest, MessageID: "m1", RequestID: "r1", SessionID: "s1", ClassroomID: "c1", SenderID: "other", SentAt: now, Deadline: now.Add(time.Minute), IdempotencyKey: "i1"}
	data, _ := json.Marshal(env)
	called := false
	server := &Server{Acceptor: &acceptOnce{stream: &readStream{Reader: strings.NewReader(string(data))}}, PeerDeviceID: "device-1", Handler: func(context.Context, protocol.Envelope) error { called = true; return nil }}
	err := server.handle(context.Background(), server.Acceptor.(*acceptOnce).stream, protocol.MaxMessageBytes)
	if !errors.Is(err, ErrPeerMismatch) || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}
