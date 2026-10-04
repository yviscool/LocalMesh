package ipc

import (
	"context"
	"errors"
	"testing"
	"time"

	"localmesh/internal/application/control"
	"localmesh/internal/domain/command"
	"localmesh/internal/protocol"
	transportipc "localmesh/internal/transport/ipc"
)

type fakeSubmitter struct {
	message protocol.Envelope
	command command.Envelope
	err     error
}

func (f *fakeSubmitter) Submit(_ context.Context, message protocol.Envelope, value command.Envelope) (control.Outcome, error) {
	f.message, f.command = message, value
	if f.err != nil {
		return control.Outcome{}, f.err
	}
	return control.Outcome{CommandID: value.CommandID}, nil
}

func validRequest() transportipc.Request {
	return transportipc.Request{ID: "request-1", MessageID: "message-1", CommandID: "command-1", SenderID: "teacher-1", SessionID: "session-1", ClassroomID: "classroom-1", IdempotencyKey: "idem-1", Capability: "process.launch", Action: "launch", TargetKind: "device", TargetID: "device-1", Deadline: time.Now().Add(time.Minute), Payload: map[string]any{"name": "notepad"}}
}

func TestServiceConvertsAndSubmitsCommand(t *testing.T) {
	fake := &fakeSubmitter{}
	service := Service{Clock: func() time.Time { return time.Unix(100, 0) }, Submitter: fake}
	response := service.Handle(context.Background(), validRequest())
	if !response.OK || response.Payload["command_id"] != "command-1" {
		t.Fatalf("response = %#v", response)
	}
	if fake.message.MessageType != protocol.MessageCommandRequest || fake.command.Capability != "process.launch" {
		t.Fatalf("message=%#v command=%#v", fake.message, fake.command)
	}
}

func TestServiceRejectsMalformedRequestBeforeSubmit(t *testing.T) {
	fake := &fakeSubmitter{}
	request := validRequest()
	request.TargetKind = "unknown"
	response := (Service{Submitter: fake}).Handle(context.Background(), request)
	if response.OK || response.Code != "invalid_request" {
		t.Fatalf("response = %#v", response)
	}
	if fake.message.MessageID != "" {
		t.Fatal("malformed request reached submitter")
	}
}

func TestServiceClassifiesAuthorizationAndIdempotencyErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		code string
	}{{"authorization", control.ErrForbidden, "unauthorized"}, {"idempotency", control.ErrIdempotencyConflict, "idempotency_conflict"}, {"other", errors.New("db down"), "ipc command submission failed"}} {
		t.Run(test.name, func(t *testing.T) {
			response := (Service{Submitter: &fakeSubmitter{err: test.err}}).Handle(context.Background(), validRequest())
			if response.Code != test.code {
				t.Fatalf("code = %q, want %q", response.Code, test.code)
			}
		})
	}
}
