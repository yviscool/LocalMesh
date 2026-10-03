package application

import (
	"context"
	"time"

	"localmesh/internal/domain/command"
	"localmesh/internal/domain/discovery"
	"localmesh/internal/domain/identity"
	"localmesh/internal/protocol"
)

type Clock interface {
	Now() time.Time
}

type DiscoveryPort interface {
	Observe(context.Context, discovery.Observation) error
	Candidates(context.Context, time.Time, time.Duration) ([]discovery.Observation, error)
}

type SessionPort interface {
	Open(context.Context, identity.DeviceID, string) (string, error)
	Close(context.Context, string, string) error
	Heartbeat(context.Context, string, time.Time) error
}

type CommandTransport interface {
	Send(context.Context, protocol.Envelope) error
}

type CommandRepository interface {
	Create(context.Context, command.Envelope) error
	RecordResult(context.Context, command.Result) error
}

type AuditSink interface {
	Append(context.Context, AuditEvent) error
}

type AuditEvent struct {
	EventID     string
	OccurredAt  time.Time
	ActorID     string
	ClassroomID string
	Action      string
	TargetKind  string
	TargetID    string
	Outcome     string
	ErrorCode   string
}
