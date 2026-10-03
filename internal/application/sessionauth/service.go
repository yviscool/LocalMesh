package sessionauth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"localmesh/internal/application"
	"localmesh/internal/auth"
	"localmesh/internal/domain/identity"
	"localmesh/internal/domain/session"
)

var (
	ErrNotConfigured     = errors.New("session authentication is not configured")
	ErrSessionIDMismatch = errors.New("challenge session id does not match requested session")
)

type Service struct {
	Clock    application.Clock
	Verifier *auth.Verifier
	Sessions application.SessionRepository
	TTL      time.Duration
}

func (s *Service) Authenticate(ctx context.Context, sessionID string, deviceID identity.DeviceID, challenge auth.Challenge, signature []byte, classroomID, transport string) (*session.Session, error) {
	if s.Verifier == nil || s.Sessions == nil || s.TTL <= 0 {
		return nil, ErrNotConfigured
	}
	if sessionID == "" || challenge.SessionID != sessionID || challenge.DeviceID != deviceID {
		return nil, ErrSessionIDMismatch
	}
	now := time.Now()
	if s.Clock != nil {
		now = s.Clock.Now()
	}
	if err := s.Verifier.Verify(challenge, signature, now); err != nil {
		return nil, fmt.Errorf("verify device challenge: %w", err)
	}
	value, err := session.New(sessionID, deviceID, now, s.TTL)
	if err != nil {
		return nil, fmt.Errorf("create authenticated session: %w", err)
	}
	if err := value.Pair(now); err != nil {
		return nil, fmt.Errorf("pair authenticated session: %w", err)
	}
	if err := value.Activate(now); err != nil {
		return nil, fmt.Errorf("activate authenticated session: %w", err)
	}
	if err := s.Sessions.OpenSession(ctx, *value, classroomID, transport); err != nil {
		return nil, fmt.Errorf("persist authenticated session: %w", err)
	}
	return value, nil
}
