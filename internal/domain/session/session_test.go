package session

import (
	"testing"
	"time"

	"localmesh/internal/domain/identity"
)

func TestSessionLifecycle(t *testing.T) {
	deviceID, err := identity.NewDeviceID("CPC-8F23-19A7")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	s, err := New("session-1", deviceID, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Pair(now); err != nil {
		t.Fatal(err)
	}
	if err := s.Activate(now); err != nil {
		t.Fatal(err)
	}
	if err := s.Heartbeat(now.Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.Heartbeat(now.Add(2 * time.Minute)); err != ErrSessionExpired {
		t.Fatalf("Heartbeat() error = %v, want %v", err, ErrSessionExpired)
	}
	if s.State != StateClosed {
		t.Fatalf("state = %s, want %s", s.State, StateClosed)
	}
}
