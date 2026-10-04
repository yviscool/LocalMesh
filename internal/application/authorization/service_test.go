package authorization

import (
	"context"
	"errors"
	"testing"
	"time"

	"localmesh/internal/domain/command"
)

func seeded() *Service {
	s := New()
	s.RegisterSession(Session{SessionID: "session-1", ActorID: "T-001", ClassroomID: "CLASS-1", ExpiresAt: time.Now().Add(time.Minute), Active: true})
	s.SetMember(Member{ActorID: "T-001", ClassroomID: "CLASS-1", Role: "teacher"})
	return s
}

func TestAllowRequiresSessionMembershipCapabilityAndScope(t *testing.T) {
	s := seeded()
	target := command.Target{Kind: command.TargetGroup, ID: "group-a"}
	if err := s.Allow(context.Background(), "session-1", "CLASS-1", "process.launch", target); err != nil {
		t.Fatal(err)
	}
	if err := s.Allow(context.Background(), "missing", "CLASS-1", "process.launch", target); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("missing session = %v", err)
	}
	if err := s.Allow(context.Background(), "session-1", "CLASS-2", "process.launch", target); !errors.Is(err, ErrTargetScope) {
		t.Fatalf("wrong classroom = %v", err)
	}
	if err := s.Allow(context.Background(), "session-1", "CLASS-1", "power.reboot", target); !errors.Is(err, ErrForbidden) {
		t.Fatalf("missing capability = %v", err)
	}
}

func TestRevokedMemberCannotControl(t *testing.T) {
	s := seeded()
	s.RevokeMember("CLASS-1", "T-001", time.Now())
	if err := s.Allow(context.Background(), "session-1", "CLASS-1", "process.launch", command.Target{Kind: command.TargetDevice, ID: "device-1"}); !errors.Is(err, ErrNotMember) {
		t.Fatalf("revoked member = %v", err)
	}
}

func TestAssistantCannotShutdown(t *testing.T) {
	s := New()
	s.RegisterSession(Session{SessionID: "session-2", ActorID: "A-001", ClassroomID: "CLASS-1", ExpiresAt: time.Now().Add(time.Minute), Active: true})
	s.SetMember(Member{ActorID: "A-001", ClassroomID: "CLASS-1", Role: "assistant"})
	if err := s.Allow(context.Background(), "session-2", "CLASS-1", "power.shutdown", command.Target{Kind: command.TargetDevice, ID: "device-1"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("assistant shutdown = %v", err)
	}
}
