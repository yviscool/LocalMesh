package application

import (
	"context"
	"testing"
	"time"

	"localmesh/internal/domain/command"
	"localmesh/internal/domain/discovery"
	"localmesh/internal/domain/identity"
	"localmesh/internal/protocol"
)

type compilePorts struct{}

func (compilePorts) Observe(context.Context, discovery.Observation) error { return nil }
func (compilePorts) Candidates(context.Context, time.Time, time.Duration) ([]discovery.Observation, error) {
	return nil, nil
}
func (compilePorts) Open(context.Context, identity.DeviceID, string) (string, error) { return "", nil }
func (compilePorts) Close(context.Context, string, string) error                     { return nil }
func (compilePorts) Heartbeat(context.Context, string, time.Time) error              { return nil }
func (compilePorts) Send(context.Context, protocol.Envelope) error                   { return nil }
func (compilePorts) Create(context.Context, command.Envelope) error                  { return nil }
func (compilePorts) RecordResult(context.Context, command.Result) error              { return nil }
func (compilePorts) Append(context.Context, AuditEvent) error                        { return nil }

func TestPortsAreExplicitBoundaries(t *testing.T) {
	var _ DiscoveryPort = compilePorts{}
	var _ SessionPort = compilePorts{}
	var _ CommandTransport = compilePorts{}
	var _ CommandRepository = compilePorts{}
	var _ AuditSink = compilePorts{}
}
