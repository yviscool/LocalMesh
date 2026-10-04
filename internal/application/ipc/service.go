package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"localmesh/internal/application/control"
	"localmesh/internal/domain/command"
	"localmesh/internal/protocol"
	transportipc "localmesh/internal/transport/ipc"
)

var (
	ErrInvalidRequest = errors.New("invalid ipc command request")
	ErrSubmitFailed   = errors.New("ipc command submission failed")
)

type Submitter interface {
	Submit(context.Context, protocol.Envelope, command.Envelope) (control.Outcome, error)
}

type Service struct {
	Clock     func() time.Time
	Submitter Submitter
}

func (s Service) Handle(ctx context.Context, request transportipc.Request) transportipc.Response {
	response := transportipc.Response{ID: request.ID}
	message, cmd, err := s.convert(request)
	if err != nil {
		response.Code = "invalid_request"
		return response
	}
	if s.Submitter == nil {
		response.Code = "ipc_not_configured"
		return response
	}
	outcome, err := s.Submitter.Submit(ctx, message, cmd)
	if err != nil {
		response.Code = classifyError(err)
		return response
	}
	response.OK = true
	response.Payload = map[string]any{"command_id": outcome.CommandID, "replayed": outcome.Replayed}
	return response
}

func (s Service) convert(request transportipc.Request) (protocol.Envelope, command.Envelope, error) {
	if request.ID == "" || request.MessageID == "" || request.CommandID == "" || request.SenderID == "" || request.SessionID == "" || request.ClassroomID == "" || request.IdempotencyKey == "" || request.Capability == "" || request.Action == "" || request.TargetKind == "" || request.TargetID == "" || request.Deadline.IsZero() {
		return protocol.Envelope{}, command.Envelope{}, ErrInvalidRequest
	}
	kind := command.TargetKind(request.TargetKind)
	switch kind {
	case command.TargetDevice, command.TargetStudent, command.TargetGroup, command.TargetClassroom:
	default:
		return protocol.Envelope{}, command.Envelope{}, ErrInvalidRequest
	}
	body, err := json.Marshal(request.Payload)
	if err != nil {
		return protocol.Envelope{}, command.Envelope{}, ErrInvalidRequest
	}
	now := time.Now()
	if s.Clock != nil {
		now = s.Clock()
	}
	message := protocol.Envelope{ProtocolVersion: protocol.CurrentVersion, MessageType: protocol.MessageCommandRequest, MessageID: request.MessageID, RequestID: request.ID, SessionID: request.SessionID, ClassroomID: request.ClassroomID, SenderID: request.SenderID, SentAt: now, Deadline: request.Deadline, IdempotencyKey: request.IdempotencyKey, Body: body}
	cmd, err := command.New(request.CommandID, request.Capability, request.Action, command.Target{Kind: kind, ID: request.TargetID}, request.Deadline, command.RetryPolicy{MaxAttempts: 1})
	if err != nil {
		return protocol.Envelope{}, command.Envelope{}, ErrInvalidRequest
	}
	return message, cmd, nil
}

func classifyError(err error) string {
	if errors.Is(err, control.ErrForbidden) || errors.Is(err, control.ErrWrongMessageType) {
		return "unauthorized"
	}
	if errors.Is(err, control.ErrIdempotencyConflict) {
		return "idempotency_conflict"
	}
	if errors.Is(err, control.ErrAuditFailed) {
		return "audit_failed"
	}
	return ErrSubmitFailed.Error()
}

var _ transportipc.Handler = Service{}
