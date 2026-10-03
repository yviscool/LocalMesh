package control

import (
	"context"
	"errors"
	"testing"
	"time"

	"localmesh/internal/application"
	"localmesh/internal/application/idempotency"
	"localmesh/internal/domain/command"
	"localmesh/internal/protocol"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type fakeAuthorizer struct{ err error }

func (a fakeAuthorizer) Allow(context.Context, string, string, string, command.Target) error {
	return a.err
}

type fakeRepository struct {
	created int
	err     error
}

func (r *fakeRepository) Create(context.Context, command.Envelope) error     { r.created++; return r.err }
func (r *fakeRepository) RecordResult(context.Context, command.Result) error { return nil }

type fakeAudit struct {
	events []application.AuditEvent
	err    error
}

func (a *fakeAudit) Append(_ context.Context, event application.AuditEvent) error {
	a.events = append(a.events, event)
	return a.err
}

func validInput() (protocol.Envelope, command.Envelope) {
	now := time.Unix(100, 0)
	message := protocol.Envelope{ProtocolVersion: protocol.CurrentVersion, MessageType: protocol.MessageCommandRequest, MessageID: "message-1", RequestID: "request-1", SessionID: "session-1", ClassroomID: "CLASS-2026-001", SenderID: "T-001", SentAt: now, Deadline: now.Add(time.Minute), IdempotencyKey: "key-1", Body: []byte("launch-vscode")}
	cmd, _ := command.New("command-1", "process.launch", "launch", command.Target{Kind: command.TargetGroup, ID: "group-a"}, now.Add(time.Minute), command.RetryPolicy{MaxAttempts: 1})
	return message, cmd
}

func newService(repo *fakeRepository, audit *fakeAudit, authorizer Authorizer) *Service {
	return &Service{Clock: fixedClock{now: time.Unix(100, 0)}, Authorizer: authorizer, Commands: repo, Audit: audit, Idempotency: idempotency.NewStore()}
}

func TestSubmitCommandCreatesAndAudits(t *testing.T) {
	repo, audit := &fakeRepository{}, &fakeAudit{}
	message, cmd := validInput()
	outcome, err := newService(repo, audit, fakeAuthorizer{}).Submit(context.Background(), message, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.CommandID != "command-1" || repo.created != 1 || len(audit.events) != 1 {
		t.Fatalf("outcome=%+v created=%d audit=%d", outcome, repo.created, len(audit.events))
	}
}

func TestSubmitCommandReplaysWithoutCreatingAgain(t *testing.T) {
	repo, audit := &fakeRepository{}, &fakeAudit{}
	message, cmd := validInput()
	service := newService(repo, audit, fakeAuthorizer{})
	if _, err := service.Submit(context.Background(), message, cmd); err != nil {
		t.Fatal(err)
	}
	outcome, err := service.Submit(context.Background(), message, cmd)
	if err != nil || !outcome.Replayed || repo.created != 1 || len(audit.events) != 1 {
		t.Fatalf("outcome=%+v err=%v created=%d audit=%d", outcome, err, repo.created, len(audit.events))
	}
}

func TestSubmitCommandRejectsAuthorizationAndFingerprintConflicts(t *testing.T) {
	repo, audit := &fakeRepository{}, &fakeAudit{}
	message, cmd := validInput()
	if _, err := newService(repo, audit, fakeAuthorizer{err: ErrForbidden}).Submit(context.Background(), message, cmd); !errors.Is(err, ErrForbidden) {
		t.Fatalf("authorization error = %v", err)
	}
	service := newService(repo, audit, fakeAuthorizer{})
	if _, err := service.Submit(context.Background(), message, cmd); err != nil {
		t.Fatal(err)
	}
	message.Body = []byte("shutdown")
	if _, err := service.Submit(context.Background(), message, cmd); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflict error = %v", err)
	}
}

func TestSubmitCommandAllowsRetryAfterRepositoryFailure(t *testing.T) {
	repo, audit := &fakeRepository{err: errors.New("temporary store failure")}, &fakeAudit{}
	message, cmd := validInput()
	service := newService(repo, audit, fakeAuthorizer{})
	if _, err := service.Submit(context.Background(), message, cmd); err == nil {
		t.Fatal("first submit should fail")
	}
	repo.err = nil
	if _, err := service.Submit(context.Background(), message, cmd); err != nil {
		t.Fatalf("retry after repository failure: %v", err)
	}
}
