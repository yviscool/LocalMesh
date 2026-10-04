package sessionrecovery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"localmesh/internal/application"
	"localmesh/internal/domain/session"
)

var ErrNotConfigured = errors.New("session recovery is not configured")

type Service struct {
	Clock    application.Clock
	Sessions application.SessionRecoveryRepository
}

// Recover closes durable sessions that cannot safely be reused. A successful
// recovery never authenticates a device; the device must perform a new
// challenge-response handshake before it can control anything.
func (s *Service) Recover(ctx context.Context) (int, error) {
	if s.Sessions == nil {
		return 0, ErrNotConfigured
	}
	now := time.Now()
	if s.Clock != nil {
		now = s.Clock.Now()
	}
	rows, err := s.Sessions.ListSessions(ctx, "")
	if err != nil {
		return 0, fmt.Errorf("list sessions: %w", err)
	}
	seen := make(map[string]string)
	closed := 0
	for _, row := range rows {
		reason := ""
		switch {
		case row.State == session.StateClosed:
			continue
		case !now.Before(row.ExpiresAt):
			reason = "expired during recovery"
		default:
			member, memberErr := s.Sessions.FindMember(ctx, row.ClassroomID, string(row.DeviceID))
			if memberErr != nil || member.RevokedAt != nil {
				reason = "membership not active during recovery"
			} else if previous, exists := seen[string(row.DeviceID)]; exists {
				reason = "superseded by newer session " + previous
			} else {
				seen[string(row.DeviceID)] = row.ID
			}
		}
		if reason != "" {
			if err := s.Sessions.CloseSession(ctx, row.ID, reason, now); err != nil {
				return closed, fmt.Errorf("close session %s: %w", row.ID, err)
			}
			closed++
		}
	}
	return closed, nil
}
