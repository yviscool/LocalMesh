package session

import (
	"errors"
	"time"

	"localmesh/internal/domain/identity"
)

var (
	ErrInvalidTransition = errors.New("invalid session transition")
	ErrSessionExpired    = errors.New("session expired")
)

type State string

const (
	StateDiscovered State = "discovered"
	StatePairing    State = "pairing"
	StateActive     State = "active"
	StateClosed     State = "closed"
)

type Session struct {
	ID        string
	DeviceID  identity.DeviceID
	State     State
	LastSeen  time.Time
	ExpiresAt time.Time
}

func New(id string, deviceID identity.DeviceID, now time.Time, ttl time.Duration) (*Session, error) {
	if id == "" || deviceID == "" || ttl <= 0 {
		return nil, errors.New("invalid session")
	}
	return &Session{ID: id, DeviceID: deviceID, State: StateDiscovered, LastSeen: now, ExpiresAt: now.Add(ttl)}, nil
}

func (s *Session) Pair(now time.Time) error {
	if s.State != StateDiscovered || s.Expired(now) {
		return ErrInvalidTransition
	}
	s.State = StatePairing
	s.LastSeen = now
	return nil
}

func (s *Session) Activate(now time.Time) error {
	if s.State != StatePairing || s.Expired(now) {
		return ErrInvalidTransition
	}
	s.State = StateActive
	s.LastSeen = now
	return nil
}

func (s *Session) Heartbeat(now time.Time) error {
	if s.State != StateActive {
		return ErrInvalidTransition
	}
	if s.Expired(now) {
		s.State = StateClosed
		return ErrSessionExpired
	}
	s.LastSeen = now
	return nil
}

func (s *Session) Close(now time.Time) {
	s.State = StateClosed
	s.LastSeen = now
}

func (s *Session) Expired(now time.Time) bool {
	return !now.Before(s.ExpiresAt)
}
