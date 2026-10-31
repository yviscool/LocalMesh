package screenlock

import (
	"strings"
	"sync"
	"testing"
)

type mockTrapper struct {
	mu        sync.Mutex
	trapping  bool
	inputOnly bool
	trapCalls int
}

func (m *mockTrapper) Trap(inputOnly bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trapping = true
	m.inputOnly = inputOnly
	m.trapCalls++
	return nil
}

func (m *mockTrapper) Untrap() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trapping = false
	m.inputOnly = false
	return nil
}

func (m *mockTrapper) IsTrapping() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.trapping
}

type mockOverlay struct {
	mu      sync.Mutex
	visible bool
	title   string
	message string
}

func (m *mockOverlay) Show(title, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.visible = true
	m.title = title
	m.message = message
	return nil
}

func (m *mockOverlay) Hide() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.visible = false
	return nil
}

func (m *mockOverlay) Destroy() error {
	return m.Hide()
}

func (m *mockOverlay) Update(title, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.title = title
	m.message = message
	return nil
}

func (m *mockOverlay) IsVisible() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.visible
}

func TestLockRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     LockRequest
		wantErr error
	}{
		{
			name: "valid standard lock",
			req: LockRequest{
				Title:         "Attention Please",
				Message:       "Teacher has locked your screen",
				LockInputOnly: false,
				ShowBanner:    true,
			},
			wantErr: nil,
		},
		{
			name: "valid empty request",
			req: LockRequest{
				LockInputOnly: true,
			},
			wantErr: nil,
		},
		{
			name: "title exceeds limit",
			req: LockRequest{
				Title:   strings.Repeat("T", MaxTitleLength+1),
				Message: "Normal message",
			},
			wantErr: ErrTitleTooLong,
		},
		{
			name: "message exceeds limit",
			req: LockRequest{
				Title:   "Normal Title",
				Message: strings.Repeat("M", MaxMessageLength+1),
			},
			wantErr: ErrMessageTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if err != tt.wantErr {
				t.Fatalf("Validate() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestLockStateTransitions(t *testing.T) {
	t.Run("basic lock and unlock cycle", func(t *testing.T) {
		trapper := &mockTrapper{}
		mgr := NewManagerWithOverlay(trapper, &mockOverlay{})

		if mgr.IsLocked() {
			t.Fatalf("expected initially unlocked")
		}
		if mgr.State() != StateUnlocked {
			t.Fatalf("expected StateUnlocked, got %v", mgr.State())
		}

		// Unlock when not locked should error
		if err := mgr.Unlock(); err != ErrNotLocked {
			t.Fatalf("expected ErrNotLocked, got %v", err)
		}

		// Normal lock
		req := LockRequest{
			Title:      "Eyes on Board",
			Message:    "Please pay attention to the teacher",
			ShowBanner: true,
		}
		if err := mgr.Lock(req); err != nil {
			t.Fatalf("Lock failed: %v", err)
		}

		if !mgr.IsLocked() {
			t.Fatalf("expected IsLocked to be true")
		}
		if mgr.State() != StateLocked {
			t.Fatalf("expected StateLocked, got %v", mgr.State())
		}
		if !trapper.IsTrapping() {
			t.Fatalf("expected trapper to be trapping")
		}

		// Second lock should fail with ErrAlreadyLocked
		if err := mgr.Lock(req); err != ErrAlreadyLocked {
			t.Fatalf("expected ErrAlreadyLocked, got %v", err)
		}

		// Update banner
		if err := mgr.UpdateBanner("New Title", "New Message"); err != nil {
			t.Fatalf("UpdateBanner failed: %v", err)
		}
		status := mgr.Status()
		if status.Request.Title != "New Title" || status.Request.Message != "New Message" {
			t.Fatalf("UpdateBanner did not update fields correctly: %+v", status.Request)
		}

		// Unlock
		if err := mgr.Unlock(); err != nil {
			t.Fatalf("Unlock failed: %v", err)
		}
		if mgr.IsLocked() {
			t.Fatalf("expected unlocked after Unlock")
		}
		if trapper.IsTrapping() {
			t.Fatalf("expected trapper to be untrapped")
		}
	})

	t.Run("lock input only mode", func(t *testing.T) {
		trapper := &mockTrapper{}
		mgr := NewManagerWithOverlay(trapper, &mockOverlay{})

		req := LockRequest{
			LockInputOnly: true,
		}
		if err := mgr.Lock(req); err != nil {
			t.Fatalf("Lock failed: %v", err)
		}
		if mgr.State() != StateInputOnly {
			t.Fatalf("expected StateInputOnly, got %v", mgr.State())
		}
		if !trapper.inputOnly {
			t.Fatalf("expected trapper inputOnly to be true")
		}
	})
}

func TestShouldTrapKey(t *testing.T) {
	tests := []struct {
		name       string
		vkCode     uint32
		flags      uint32
		isCtrlDown bool
		fullLock   bool
		wantTrap   bool
	}{
		// Full lock intercepts all keys
		{
			name:       "full lock intercepts normal key A",
			vkCode:     0x41, // 'A'
			flags:      0,
			isCtrlDown: false,
			fullLock:   true,
			wantTrap:   true,
		},
		{
			name:       "full lock intercepts return",
			vkCode:     0x0D, // Enter
			flags:      0,
			isCtrlDown: false,
			fullLock:   true,
			wantTrap:   true,
		},

		// Shortcut trapping (partial lock / input trapping)
		{
			name:       "intercepts Ctrl+Esc",
			vkCode:     VK_ESCAPE,
			flags:      0,
			isCtrlDown: true,
			fullLock:   false,
			wantTrap:   true,
		},
		{
			name:       "allows Esc alone without Ctrl",
			vkCode:     VK_ESCAPE,
			flags:      0,
			isCtrlDown: false,
			fullLock:   false,
			wantTrap:   false,
		},
		{
			name:       "intercepts Alt+Tab",
			vkCode:     VK_TAB,
			flags:      LLKHF_ALTDOWN,
			isCtrlDown: false,
			fullLock:   false,
			wantTrap:   true,
		},
		{
			name:       "allows Tab alone without Alt",
			vkCode:     VK_TAB,
			flags:      0,
			isCtrlDown: false,
			fullLock:   false,
			wantTrap:   false,
		},
		{
			name:       "intercepts Alt+F4",
			vkCode:     VK_F4,
			flags:      LLKHF_ALTDOWN,
			isCtrlDown: false,
			fullLock:   false,
			wantTrap:   true,
		},
		{
			name:       "intercepts Alt+Esc",
			vkCode:     VK_ESCAPE,
			flags:      LLKHF_ALTDOWN,
			isCtrlDown: false,
			fullLock:   false,
			wantTrap:   true,
		},
		{
			name:       "intercepts Alt+Space",
			vkCode:     VK_SPACE,
			flags:      LLKHF_ALTDOWN,
			isCtrlDown: false,
			fullLock:   false,
			wantTrap:   true,
		},
		{
			name:       "intercepts Left Windows Key",
			vkCode:     VK_LWIN,
			flags:      0,
			isCtrlDown: false,
			fullLock:   false,
			wantTrap:   true,
		},
		{
			name:       "intercepts Right Windows Key",
			vkCode:     VK_RWIN,
			flags:      0,
			isCtrlDown: false,
			fullLock:   false,
			wantTrap:   true,
		},
		{
			name:       "allows normal typing when fullLock is false",
			vkCode:     0x42, // 'B'
			flags:      0,
			isCtrlDown: false,
			fullLock:   false,
			wantTrap:   false,
		},
		{
			name:       "allows Alt+X (arbitrary alt combination) when fullLock is false",
			vkCode:     0x58, // 'X'
			flags:      LLKHF_ALTDOWN,
			isCtrlDown: false,
			fullLock:   false,
			wantTrap:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trapped := ShouldTrapKey(tt.vkCode, tt.flags, tt.isCtrlDown, tt.fullLock)
			if trapped != tt.wantTrap {
				t.Errorf("ShouldTrapKey() = %v, want %v", trapped, tt.wantTrap)
			}
		})
	}
}

func TestManagerConcurrency(t *testing.T) {
	trapper := &mockTrapper{}
	mgr := NewManagerWithOverlay(trapper, &mockOverlay{})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = mgr.Lock(LockRequest{Title: "Concurrent Lock"})
		}()
		go func() {
			defer wg.Done()
			_ = mgr.Unlock()
		}()
	}
	wg.Wait()

	// Ensure final state can be cleanly queried
	_ = mgr.Status()
}
