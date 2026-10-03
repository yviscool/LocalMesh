package tcp

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"testing"
	"time"

	"localmesh/internal/protocol"
)

func testMessage() protocol.Envelope {
	now := time.Now()
	return protocol.Envelope{ProtocolVersion: protocol.CurrentVersion, MessageType: protocol.MessageSessionHeartbeat, MessageID: "message-1", RequestID: "request-1", SenderID: "device-1", SentAt: now, Deadline: now.Add(time.Minute), Body: []byte("heartbeat")}
}

func TestTransportRoundTrip(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	received := make(chan protocol.Envelope, 1)
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- ListenAndServe(ctx, listener, func(_ context.Context, message protocol.Envelope) error {
			received <- message
			return nil
		})
	}()
	transport := Transport{Address: listener.Addr().String(), DialTimeout: time.Second, WriteTimeout: time.Second}
	message := testMessage()
	if err := transport.Send(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-received:
		if got.MessageID != message.MessageID || string(got.Body) != string(message.Body) {
			t.Fatalf("received = %+v, want %+v", got, message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
	}
	cancel()
	select {
	case err := <-serverDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("server error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop")
	}
}

func TestReadMessageRejectsOversizedFrame(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	go func() {
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], MaxFrameBytes+1)
		_, _ = left.Write(header[:])
	}()
	_, err := readMessage(context.Background(), right)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("readMessage() error = %v, want %v", err, ErrFrameTooLarge)
	}
}
