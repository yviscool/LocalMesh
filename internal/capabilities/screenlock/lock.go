package screenlock

import (
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrAlreadyLocked  = errors.New("screen is already locked")
	ErrNotLocked      = errors.New("screen is not locked")
	ErrInvalidRequest = errors.New("invalid lock request")
	ErrMessageTooLong = errors.New("lock message exceeds maximum length")
	ErrTitleTooLong   = errors.New("lock title exceeds maximum length")
)

const (
	MaxMessageLength = 1024
	MaxTitleLength   = 128

	// Virtual-key codes from Win32
	VK_TAB     = 0x09
	VK_ESCAPE  = 0x1B
	VK_SPACE   = 0x20
	VK_CONTROL = 0x11
	VK_LWIN    = 0x5B
	VK_RWIN    = 0x5C
	VK_F4      = 0x73

	// LLKHF_ALTDOWN represents the transition-state flag in KBDLLHOOKSTRUCT
	LLKHF_ALTDOWN = 0x20
)

type LockState string

const (
	StateUnlocked  LockState = "unlocked"
	StateLocked    LockState = "locked"
	StateInputOnly LockState = "input_only"
)

// LockRequest defines parameters for locking a student's screen and input.
type LockRequest struct {
	Title         string `json:"title"`
	Message       string `json:"message"`
	LockInputOnly bool   `json:"lock_input_only"`
	ShowBanner    bool   `json:"show_banner"`
}

// Validate validates request bounds.
func (r LockRequest) Validate() error {
	if len(r.Title) > MaxTitleLength {
		return ErrTitleTooLong
	}
	if len(r.Message) > MaxMessageLength {
		return ErrMessageTooLong
	}
	return nil
}

// LockStatus returns the current state and metadata of the screen lock.
type LockStatus struct {
	State      LockState   `json:"state"`
	Request    LockRequest `json:"request,omitempty"`
	LockedAt   time.Time   `json:"locked_at,omitempty"`
	UnlockedAt time.Time   `json:"unlocked_at,omitempty"`
	IsTrapping bool        `json:"is_trapping"`
}

// Trapper abstracts platform-specific low-level keyboard/mouse hooks and input blocking.
type Trapper interface {
	Trap(inputOnly bool) error
	Untrap() error
	IsTrapping() bool
}

// Overlay abstracts platform-specific full-screen black overlay rendering.
type Overlay interface {
	Show(title, message string) error
	Hide() error
	Destroy() error
	Update(title, message string) error
	IsVisible() bool
}

// Manager coordinates screen locking and shortcut trapping state machines.
type Manager struct {
	mu         sync.RWMutex
	state      LockState
	request    LockRequest
	trapper    Trapper
	overlay    Overlay
	lockedAt   time.Time
	unlockedAt time.Time
}

// NewManager creates a screen lock manager with the specified trapper.
// If trapper is nil, NewPlatformTrapper() will be used.
func NewManager(trapper Trapper) *Manager {
	return NewManagerWithOverlay(trapper, nil)
}

// NewManagerWithOverlay creates a screen lock manager with specified trapper and overlay.
func NewManagerWithOverlay(trapper Trapper, overlay Overlay) *Manager {
	if trapper == nil {
		trapper = NewPlatformTrapper()
	}
	if overlay == nil {
		overlay = NewPlatformOverlay()
	}
	return &Manager{
		state:   StateUnlocked,
		trapper: trapper,
		overlay: overlay,
	}
}

// SetOverlay allows substituting an overlay implementation (e.g. for testing).
func (m *Manager) SetOverlay(overlay Overlay) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.overlay = overlay
}

// Lock engages screen and/or input locking according to the request.
func (m *Manager) Lock(req LockRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state != StateUnlocked {
		return ErrAlreadyLocked
	}

	targetState := StateLocked
	if req.LockInputOnly {
		targetState = StateInputOnly
	}

	if m.trapper != nil {
		if err := m.trapper.Trap(req.LockInputOnly); err != nil {
			return err
		}
	}

	if !req.LockInputOnly && m.overlay != nil {
		_ = m.overlay.Show(req.Title, req.Message)
	}

	m.state = targetState
	m.request = req
	m.lockedAt = time.Now()
	m.unlockedAt = time.Time{}
	return nil
}

// Unlock releases the screen lock and restores input.
func (m *Manager) Unlock() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == StateUnlocked {
		return ErrNotLocked
	}

	if m.overlay != nil {
		_ = m.overlay.Hide()
	}

	if m.trapper != nil {
		if err := m.trapper.Untrap(); err != nil {
			return err
		}
	}

	m.state = StateUnlocked
	m.request = LockRequest{}
	m.unlockedAt = time.Now()
	return nil
}

// UpdateBanner updates the title and message while locked.
func (m *Manager) UpdateBanner(title, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == StateUnlocked {
		return ErrNotLocked
	}

	req := m.request
	req.Title = strings.TrimSpace(title)
	req.Message = strings.TrimSpace(message)

	if err := req.Validate(); err != nil {
		return err
	}

	m.request = req
	if m.overlay != nil && m.overlay.IsVisible() {
		_ = m.overlay.Update(req.Title, req.Message)
	}
	return nil
}

// State returns the current lock state.
func (m *Manager) State() LockState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// IsLocked reports whether the screen is currently locked or input-only locked.
func (m *Manager) IsLocked() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state != StateUnlocked
}

// Status returns a snapshot of lock status.
func (m *Manager) Status() LockStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	isTrapping := false
	if m.trapper != nil {
		isTrapping = m.trapper.IsTrapping()
	}

	return LockStatus{
		State:      m.state,
		Request:    m.request,
		LockedAt:   m.lockedAt,
		UnlockedAt: m.unlockedAt,
		IsTrapping: isTrapping,
	}
}

// ShouldTrapKey evaluates whether a keyboard event should be intercepted and suppressed.
// Following Veyon WindowsKeyboardShortcutTrapper:
// - Alt+Tab
// - Alt+F4
// - Alt+Esc
// - Alt+Space
// - Ctrl+Esc
// - Windows keys (VK_LWIN, VK_RWIN)
// When fullLock is true, all keys are suppressed.
func ShouldTrapKey(vkCode uint32, flags uint32, isCtrlDown bool, fullLock bool) bool {
	if fullLock {
		return true
	}

	// Ctrl + Esc (Task Manager / Start Menu shortcut)
	if vkCode == VK_ESCAPE && isCtrlDown {
		return true
	}

	// Alt + key shortcuts
	if (flags & LLKHF_ALTDOWN) != 0 {
		switch vkCode {
		case VK_TAB, VK_ESCAPE, VK_SPACE, VK_F4:
			return true
		}
	}

	// Windows keys (Start Menu / Win+X / Win+R / etc.)
	if vkCode == VK_LWIN || vkCode == VK_RWIN {
		return true
	}

	return false
}
