package quic

import (
	"context"
	"io"
	"testing"
	"time"

	"localmesh/internal/protocol"
)

type fakeStream struct {
	data     []byte
	deadline time.Time
}

func (s *fakeStream) Read([]byte) (int, error)      { return 0, io.EOF }
func (s *fakeStream) Write(p []byte) (int, error)   { s.data = append(s.data, p...); return len(p), nil }
func (s *fakeStream) Close() error                  { return nil }
func (s *fakeStream) SetDeadline(v time.Time) error { s.deadline = v; return nil }

type fakeOpener struct{ stream *fakeStream }

func (o fakeOpener) Open(context.Context) (Stream, error) { return o.stream, nil }

func TestClientSendsValidatedEnvelopeAndSetsDeadline(t *testing.T) {
	stream := &fakeStream{}
	client := &Client{Opener: fakeOpener{stream}, PeerDeviceID: "teacher-1"}
	now := time.Now()
	env := protocol.Envelope{ProtocolVersion: protocol.CurrentVersion, MessageType: protocol.MessageCommandRequest, MessageID: "m1", RequestID: "r1", SessionID: "s1", ClassroomID: "c1", SenderID: "teacher-1", SentAt: now, Deadline: now.Add(time.Minute), IdempotencyKey: "i1"}
	if err := client.Send(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if stream.deadline.IsZero() || len(stream.data) == 0 {
		t.Fatalf("deadline=%v bytes=%d", stream.deadline, len(stream.data))
	}
}

func TestClientRejectsPeerMismatchAndInvalidEnvelope(t *testing.T) {
	client := &Client{Opener: fakeOpener{&fakeStream{}}, PeerDeviceID: "device-1"}
	env := protocol.Envelope{SenderID: "other"}
	if err := client.Send(context.Background(), env); err != ErrPeerMismatch {
		t.Fatalf("mismatch=%v", err)
	}
	client.PeerDeviceID = ""
	if err := client.Send(context.Background(), env); err == nil {
		t.Fatal("invalid envelope accepted")
	}
}
