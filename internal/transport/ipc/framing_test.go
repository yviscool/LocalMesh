package ipc

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
)

type handler struct{}

func (handler) Handle(_ context.Context, request Request) Response {
	return Response{ID: request.ID, OK: request.Capability == "process.control"}
}

type authorizer struct{ allow bool }

func (a authorizer) Allow(context.Context, string, string, string, string, string) error {
	if !a.allow {
		return errors.New("forbidden")
	}
	return nil
}

func TestCapabilityHandlerRechecksAuthorization(t *testing.T) {
	request := Request{ID: "r1", SessionID: "s1", ClassroomID: "c1", Capability: "process.control", TargetKind: "device", TargetID: "d1"}
	denied := (CapabilityHandler{Authorizer: authorizer{}, Next: handler{}}).Handle(context.Background(), request)
	if denied.Code != "unauthorized" {
		t.Fatalf("denied = %#v", denied)
	}
	allowed := (CapabilityHandler{Authorizer: authorizer{allow: true}, Next: handler{}}).Handle(context.Background(), request)
	if !allowed.OK {
		t.Fatalf("allowed = %#v", allowed)
	}
}

func TestFramingHandlesPartialReadsAndDuplicateRequests(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), right, handler{}, 1024) }()
	request := Request{ID: "req-1", Capability: "process.control", Action: "pause"}
	go func() { _ = Write(left, request, 1024); _ = Write(left, request, 1024) }()
	var response Response
	if err := Read(left, &response, 1024); err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.ID != request.ID {
		t.Fatalf("response = %#v", response)
	}
	if err := Read(left, &response, 1024); err != nil {
		t.Fatal(err)
	}
	if response.Code != ErrDuplicateRequest.Error() {
		t.Fatalf("duplicate response = %#v", response)
	}
	_ = left.Close()
	if err := <-done; !errors.Is(err, io.EOF) && err != nil {
		t.Fatalf("serve error = %v", err)
	}
}

func TestFramingRejectsOversizedMessages(t *testing.T) {
	if err := Write(io.Discard, Request{ID: "x", Payload: map[string]any{"data": "123456"}}, 16); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("Write() error = %v", err)
	}
}
