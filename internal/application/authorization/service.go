package authorization

import (
	"context"
	"errors"
	"sync"
	"time"

	"localmesh/internal/domain/command"
)

var (
	ErrUnauthenticated = errors.New("session is not active")
	ErrNotMember       = errors.New("actor is not a classroom member")
	ErrForbidden       = errors.New("capability is not allowed")
	ErrTargetScope     = errors.New("target is outside classroom scope")
)

type Session struct {
	SessionID   string
	ActorID     string
	ClassroomID string
	ExpiresAt   time.Time
	Active      bool
}

type Member struct {
	ActorID     string
	ClassroomID string
	Role        string
	RevokedAt   time.Time
}

type Service struct {
	mu           sync.RWMutex
	sessions     map[string]Session
	members      map[string]Member
	capabilities map[string]map[string]bool
}

func New() *Service {
	return &Service{sessions: make(map[string]Session), members: make(map[string]Member), capabilities: map[string]map[string]bool{
		"teacher":   {"classroom.control": true, "process.launch": true, "process.kill": true, "screen.view": true, "screen.broadcast": true, "file.send": true, "power.shutdown": true},
		"assistant": {"classroom.control": true, "process.launch": true, "screen.view": true, "screen.broadcast": true, "file.send": true},
		"student":   {"screen.view": false},
	}}
}

func (s *Service) RegisterSession(value Session) {
	s.mu.Lock()
	s.sessions[value.SessionID] = value
	s.mu.Unlock()
}

func (s *Service) CloseSession(sessionID string) {
	s.mu.Lock()
	value, ok := s.sessions[sessionID]
	if ok {
		value.Active = false
		s.sessions[sessionID] = value
	}
	s.mu.Unlock()
}

func (s *Service) SetMember(value Member) {
	s.mu.Lock()
	s.members[value.ClassroomID+"\x00"+value.ActorID] = value
	s.mu.Unlock()
}

func (s *Service) RevokeMember(classroomID, actorID string, revokedAt time.Time) {
	s.mu.Lock()
	key := classroomID + "\x00" + actorID
	value, ok := s.members[key]
	if ok {
		value.RevokedAt = revokedAt
		s.members[key] = value
	}
	s.mu.Unlock()
}

func (s *Service) Allow(_ context.Context, sessionID, classroomID, capability string, target command.Target) error {
	now := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	sessionValue, ok := s.sessions[sessionID]
	if !ok || !sessionValue.Active || !now.Before(sessionValue.ExpiresAt) {
		return ErrUnauthenticated
	}
	if sessionValue.ClassroomID != classroomID {
		return ErrTargetScope
	}
	member, ok := s.members[classroomID+"\x00"+sessionValue.ActorID]
	if !ok || !member.RevokedAt.IsZero() {
		return ErrNotMember
	}
	if !s.capabilities[member.Role][capability] {
		return ErrForbidden
	}
	if target.ID == "" || target.Kind == "" {
		return ErrTargetScope
	}
	if target.Kind == command.TargetClassroom && target.ID != classroomID {
		return ErrTargetScope
	}
	return nil
}
