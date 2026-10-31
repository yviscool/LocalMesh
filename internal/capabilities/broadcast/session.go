package broadcast

import (
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrSessionNotActive      = errors.New("broadcast session is not active")
	ErrUnauthorizedSender    = errors.New("sender is not authorized for current broadcast mode")
	ErrTeacherIDRequired     = errors.New("teacher device ID is required")
	ErrStudentIDRequired     = errors.New("student device ID is required")
	ErrInvalidModeTransition = errors.New("cannot perform mode switch on inactive or stopped session")
	ErrSessionAlreadyActive  = errors.New("broadcast session is already running")
)

// BroadcastMode indicates who is authorized to broadcast screen streams.
type BroadcastMode string

const (
	BroadcastModeIdle        BroadcastMode = "idle"
	BroadcastModeTeacher     BroadcastMode = "teacher"
	BroadcastModeStudentDemo BroadcastMode = "student_demo"
	BroadcastModeStopped     BroadcastMode = "stopped"
)

// ModeChangedHandler receives notifications when broadcast role or sender changes.
type ModeChangedHandler func(oldMode, newMode BroadcastMode, activeSender string)

// SessionSnapshot contains an immutable point-in-time state of a BroadcastSession.
type SessionSnapshot struct {
	ClassroomID     string        `json:"classroom_id"`
	Mode            BroadcastMode `json:"mode"`
	TeacherDeviceID string        `json:"teacher_device_id"`
	StudentDeviceID string        `json:"student_device_id,omitempty"`
	ActiveSenderID  string        `json:"active_sender_id"`
	IsActive        bool          `json:"is_active"`
	StartedAt       time.Time     `json:"started_at"`
}

// BroadcastSession manages classroom screen broadcast state and role switches
// between normal Teacher Broadcast and Student Demo.
// In accordance with LocalMesh security rules, discovery never grants control:
// incoming frames MUST match ActiveSenderID.
type BroadcastSession struct {
	mu              sync.RWMutex
	classroomID     string
	mode            BroadcastMode
	teacherDeviceID string
	studentDeviceID string
	activeSenderID  string
	startedAt       time.Time
	onModeChanged   ModeChangedHandler
}

// NewBroadcastSession creates a new BroadcastSession in Idle mode.
func NewBroadcastSession(classroomID string) *BroadcastSession {
	return &BroadcastSession{
		classroomID: strings.TrimSpace(classroomID),
		mode:        BroadcastModeIdle,
	}
}

// StartTeacherBroadcast initiates a Teacher Broadcast session.
func (s *BroadcastSession) StartTeacherBroadcast(teacherDeviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	trimmed := strings.TrimSpace(teacherDeviceID)
	if trimmed == "" {
		return ErrTeacherIDRequired
	}

	oldMode := s.mode
	s.mode = BroadcastModeTeacher
	s.teacherDeviceID = trimmed
	s.studentDeviceID = ""
	s.activeSenderID = trimmed
	s.startedAt = time.Now()

	if s.onModeChanged != nil {
		s.onModeChanged(oldMode, s.mode, s.activeSenderID)
	}

	return nil
}

// SwitchToStudentDemo transfers the broadcasting role to a student ("学生演示").
// The session must already be active (either Teacher or another Student Demo).
func (s *BroadcastSession) SwitchToStudentDemo(studentDeviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.mode != BroadcastModeTeacher && s.mode != BroadcastModeStudentDemo {
		return ErrInvalidModeTransition
	}

	trimmed := strings.TrimSpace(studentDeviceID)
	if trimmed == "" {
		return ErrStudentIDRequired
	}

	oldMode := s.mode
	s.mode = BroadcastModeStudentDemo
	s.studentDeviceID = trimmed
	s.activeSenderID = trimmed

	if s.onModeChanged != nil {
		s.onModeChanged(oldMode, s.mode, s.activeSenderID)
	}

	return nil
}

// SwitchToTeacher restores broadcasting control back to the teacher.
// The session must already be active.
func (s *BroadcastSession) SwitchToTeacher() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.mode != BroadcastModeTeacher && s.mode != BroadcastModeStudentDemo {
		return ErrInvalidModeTransition
	}
	if s.teacherDeviceID == "" {
		return ErrTeacherIDRequired
	}

	oldMode := s.mode
	s.mode = BroadcastModeTeacher
	s.studentDeviceID = ""
	s.activeSenderID = s.teacherDeviceID

	if s.onModeChanged != nil {
		s.onModeChanged(oldMode, s.mode, s.activeSenderID)
	}

	return nil
}

// Stop terminates the broadcast session.
func (s *BroadcastSession) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldMode := s.mode
	s.mode = BroadcastModeStopped
	s.activeSenderID = ""

	if s.onModeChanged != nil {
		s.onModeChanged(oldMode, s.mode, "")
	}

	return nil
}

// IsActive returns whether the session is currently broadcasting.
func (s *BroadcastSession) IsActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mode == BroadcastModeTeacher || s.mode == BroadcastModeStudentDemo
}

// Mode returns the current BroadcastMode.
func (s *BroadcastSession) Mode() BroadcastMode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mode
}

// ActiveSenderID returns the device ID currently authorized to stream.
func (s *BroadcastSession) ActiveSenderID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeSenderID
}

// TeacherDeviceID returns the registered teacher device ID.
func (s *BroadcastSession) TeacherDeviceID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.teacherDeviceID
}

// StudentDeviceID returns the active student demo device ID (if in student demo mode).
func (s *BroadcastSession) StudentDeviceID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.studentDeviceID
}

// ClassroomID returns the associated classroom ID.
func (s *BroadcastSession) ClassroomID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.classroomID
}

// IsAuthorizedSender checks if a device is currently authorized to stream frames.
func (s *BroadcastSession) IsAuthorizedSender(deviceID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.mode != BroadcastModeTeacher && s.mode != BroadcastModeStudentDemo {
		return false
	}
	trimmed := strings.TrimSpace(deviceID)
	return trimmed != "" && trimmed == s.activeSenderID
}

// ValidateFrame validates whether an incoming frame originates from the authorized sender.
// LocalMesh rule: explicit authorization required; unauthorized frames are rejected.
func (s *BroadcastSession) ValidateFrame(frame ScreenFrame) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.mode != BroadcastModeTeacher && s.mode != BroadcastModeStudentDemo {
		return ErrSessionNotActive
	}

	trimmedSource := strings.TrimSpace(frame.SourceDeviceID)
	if trimmedSource == "" || trimmedSource != s.activeSenderID {
		return ErrUnauthorizedSender
	}

	return nil
}

// SetOnModeChanged registers a callback invoked on broadcast mode transitions.
func (s *BroadcastSession) SetOnModeChanged(fn ModeChangedHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onModeChanged = fn
}

// Snapshot returns a copy of the current session state.
func (s *BroadcastSession) Snapshot() SessionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return SessionSnapshot{
		ClassroomID:     s.classroomID,
		Mode:            s.mode,
		TeacherDeviceID: s.teacherDeviceID,
		StudentDeviceID: s.studentDeviceID,
		ActiveSenderID:  s.activeSenderID,
		IsActive:        s.mode == BroadcastModeTeacher || s.mode == BroadcastModeStudentDemo,
		StartedAt:       s.startedAt,
	}
}
