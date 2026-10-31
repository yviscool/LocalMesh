package watchdog

import (
	"context"
	"sync"
	"testing"
	"time"

	"localmesh/internal/capabilities/screenlock"
)

type mockTrapper struct {
	mu       sync.Mutex
	trapping bool
}

func (m *mockTrapper) Trap(_ bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trapping = true
	return nil
}

func (m *mockTrapper) Untrap() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trapping = false
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

func setupTestGuard(lease, interval time.Duration) (*OfflineGuard, *screenlock.Manager, *mockOverlay) {
	trapper := &mockTrapper{}
	overlay := &mockOverlay{}
	lockMgr := screenlock.NewManagerWithOverlay(trapper, overlay)

	cfg := OfflineGuardConfig{
		LeaseDuration:  lease,
		CheckInterval:  interval,
		WarningTitle:   DefaultWarningTitle,
		WarningMessage: DefaultWarningMessage,
		CheckNICState:  false, // disable physical check in basic unit tests
	}
	guard := NewOfflineGuard(lockMgr, cfg)
	return guard, lockMgr, overlay
}

func TestOfflineGuard_NotInSessionNoLock(t *testing.T) {
	guard, lockMgr, _ := setupTestGuard(50*time.Millisecond, 10*time.Millisecond)

	time.Sleep(100 * time.Millisecond)
	guard.Check()

	if guard.IsPunished() {
		t.Errorf("expected not punished when not in session")
	}
	if lockMgr.Status().State != screenlock.StateUnlocked {
		t.Errorf("expected lockMgr unlocked, got %v", lockMgr.Status().State)
	}
}

func TestOfflineGuard_LockOnLeaseExpiration(t *testing.T) {
	guard, lockMgr, overlay := setupTestGuard(50*time.Millisecond, 10*time.Millisecond)

	guard.EnterSession("CLASS-101")
	if !guard.InSession() {
		t.Fatalf("expected in session")
	}

	// Wait for lease to expire
	time.Sleep(80 * time.Millisecond)
	guard.Check()

	if !guard.IsPunished() {
		t.Errorf("expected punished after lease timeout")
	}

	status := lockMgr.Status()
	if status.State != screenlock.StateLocked {
		t.Errorf("expected lock status StateLocked, got %v", status.State)
	}
	if status.Request.Title != DefaultWarningTitle {
		t.Errorf("expected warning title %q, got %q", DefaultWarningTitle, status.Request.Title)
	}
	if status.Request.Message != DefaultWarningMessage {
		t.Errorf("expected warning message %q, got %q", DefaultWarningMessage, status.Request.Message)
	}

	overlay.mu.Lock()
	if !overlay.visible || overlay.title != DefaultWarningTitle || overlay.message != DefaultWarningMessage {
		t.Errorf("expected overlay visible with warning text, got visible=%v title=%q", overlay.visible, overlay.title)
	}
	overlay.mu.Unlock()
}

func TestOfflineGuard_AutoUnlockOnHeartbeat(t *testing.T) {
	guard, lockMgr, overlay := setupTestGuard(50*time.Millisecond, 10*time.Millisecond)

	guard.EnterSession("CLASS-101")
	time.Sleep(80 * time.Millisecond)
	guard.Check()

	if !guard.IsPunished() {
		t.Fatalf("expected punished before heartbeat")
	}

	// Pulse arrives from teacher
	guard.RecordHeartbeat()

	if guard.IsPunished() {
		t.Errorf("expected un-punished after heartbeat")
	}
	if lockMgr.Status().State != screenlock.StateUnlocked {
		t.Errorf("expected lock status StateUnlocked, got %v", lockMgr.Status().State)
	}

	overlay.mu.Lock()
	if overlay.visible {
		t.Errorf("expected overlay hidden after heartbeat")
	}
	overlay.mu.Unlock()
}

func TestOfflineGuard_ContinuousHeartbeatsPreventLock(t *testing.T) {
	guard, lockMgr, _ := setupTestGuard(100*time.Millisecond, 20*time.Millisecond)

	guard.EnterSession("CLASS-101")

	for i := 0; i < 5; i++ {
		time.Sleep(40 * time.Millisecond)
		guard.RecordHeartbeat()
		guard.Check()
		if guard.IsPunished() {
			t.Fatalf("unexpected lock at iteration %d", i)
		}
	}

	if lockMgr.Status().State != screenlock.StateUnlocked {
		t.Errorf("expected remaining unlocked")
	}
}

func TestOfflineGuard_CarrierLossImmediateLock(t *testing.T) {
	guard, lockMgr, _ := setupTestGuard(10*time.Second, 10*time.Millisecond)

	hasCarrier := true
	guard.cfg.CheckNICState = true
	guard.SetNetChecker(func() bool {
		return hasCarrier
	})

	guard.EnterSession("CLASS-101")
	guard.Check()
	if guard.IsPunished() {
		t.Fatalf("expected not punished while carrier is up")
	}

	// Simulate unplugging cable
	hasCarrier = false
	guard.Check()

	if !guard.IsPunished() {
		t.Errorf("expected punished immediately when network cable is unplugged")
	}
	if lockMgr.Status().State != screenlock.StateLocked {
		t.Errorf("expected screen locked on carrier loss")
	}
}

func TestOfflineGuard_LeaveSessionClearsLock(t *testing.T) {
	guard, lockMgr, _ := setupTestGuard(50*time.Millisecond, 10*time.Millisecond)

	guard.EnterSession("CLASS-101")
	time.Sleep(80 * time.Millisecond)
	guard.Check()

	if !guard.IsPunished() {
		t.Fatalf("expected punished")
	}

	guard.LeaveSession()

	if guard.IsPunished() {
		t.Errorf("expected punishment cleared on LeaveSession")
	}
	if guard.InSession() {
		t.Errorf("expected not InSession")
	}
	if lockMgr.Status().State != screenlock.StateUnlocked {
		t.Errorf("expected unlocked after LeaveSession")
	}
}

func TestOfflineGuard_BackgroundRoutine(t *testing.T) {
	guard, lockMgr, _ := setupTestGuard(60*time.Millisecond, 15*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	guard.Start(ctx)
	guard.EnterSession("CLASS-101")

	// Wait for background timer to trigger check and lock
	time.Sleep(150 * time.Millisecond)

	if !guard.IsPunished() {
		t.Errorf("expected background monitor to trigger punishment")
	}
	if lockMgr.Status().State != screenlock.StateLocked {
		t.Errorf("expected screen locked by background monitor")
	}

	// Now stop guard
	guard.Stop()

	if guard.IsPunished() {
		t.Errorf("expected Stop to clear punishment")
	}
	if lockMgr.Status().State != screenlock.StateUnlocked {
		t.Errorf("expected screen unlocked after Stop")
	}
}

func TestOfflineGuard_ConcurrentAccess(t *testing.T) {
	guard, _, _ := setupTestGuard(20*time.Millisecond, 5*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	guard.Start(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if j%4 == 0 {
					guard.EnterSession("CLASS-CONCURRENT")
				} else if j%4 == 1 {
					guard.RecordHeartbeat()
				} else if j%4 == 2 {
					guard.Check()
				} else {
					guard.LeaveSession()
				}
			}
		}(i)
	}

	wg.Wait()
	guard.Stop()
}
