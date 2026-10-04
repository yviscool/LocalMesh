package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"localmesh/internal/application"
	"localmesh/internal/application/idempotency"
	"localmesh/internal/domain/command"
	"localmesh/internal/protocol"
)

var (
	ErrWrongMessageType    = errors.New("message is not a command request")
	ErrForbidden           = errors.New("command is not authorized")
	ErrIdempotencyConflict = errors.New("idempotency key conflict")
	ErrAuditFailed         = errors.New("audit write failed")
)

type Authorizer interface {
	Allow(context.Context, string, string, string, command.Target) error
}

type Service struct {
	Clock       application.Clock
	Authorizer  Authorizer
	Commands    application.CommandRepository
	Audit       application.AuditSink
	Idempotency *idempotency.Store
}

type Outcome struct {
	CommandID string
	Replayed  bool
}

func (s *Service) Submit(ctx context.Context, message protocol.Envelope, cmd command.Envelope) (Outcome, error) {
	now := time.Now()
	if s.Clock != nil {
		now = s.Clock.Now()
	}
	if err := message.Validate(now); err != nil {
		return Outcome{}, fmt.Errorf("validate command message: %w", err)
	}
	if message.MessageType != protocol.MessageCommandRequest {
		return Outcome{}, ErrWrongMessageType
	}
	if s.Authorizer == nil || s.Commands == nil || s.Audit == nil || s.Idempotency == nil {
		return Outcome{}, errors.New("command service is not configured")
	}
	if err := s.Authorizer.Allow(ctx, message.SessionID, message.ClassroomID, cmd.Capability, cmd.Target); err != nil {
		return Outcome{}, fmt.Errorf("authorize command: %w", err)
	}
	fingerprint := commandFingerprint(message, cmd)
	cmd.RequestID = message.RequestID
	cmd.IdempotencyKey = message.IdempotencyKey
	cmd.ActorID = message.SenderID
	cmd.ClassroomID = message.ClassroomID
	reservation := s.Idempotency.Begin(message.IdempotencyKey, fingerprint)
	switch reservation.Status {
	case idempotency.StatusConflict:
		return Outcome{}, ErrIdempotencyConflict
	case idempotency.StatusReplayed:
		return Outcome{CommandID: cmd.CommandID, Replayed: true}, nil
	}
	if err := s.Commands.Create(ctx, cmd); err != nil {
		s.Idempotency.Abort(message.IdempotencyKey, fingerprint)
		return Outcome{}, fmt.Errorf("create command: %w", err)
	}
	event := application.AuditEvent{EventID: message.MessageID, OccurredAt: now, ActorID: message.SenderID, ClassroomID: message.ClassroomID, Action: cmd.Name, TargetKind: string(cmd.Target.Kind), TargetID: cmd.Target.ID, Outcome: "accepted"}
	if err := s.Audit.Append(ctx, event); err != nil {
		return Outcome{}, fmt.Errorf("%w: %v", ErrAuditFailed, err)
	}
	s.Idempotency.Complete(message.IdempotencyKey, fingerprint, []byte(cmd.CommandID))
	return Outcome{CommandID: cmd.CommandID}, nil
}

func commandFingerprint(message protocol.Envelope, cmd command.Envelope) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "%s|%s|%s|%s|%s|%s|%x", message.MessageType, message.ClassroomID, message.SenderID, cmd.Capability, cmd.Name, cmd.Target.ID, message.Body)
	return hex.EncodeToString(hash.Sum(nil))
}
