package watchdog

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockWatcher implements ProcessWatcher with configurable exit signals.
type mockWatcher struct {
	mu        sync.Mutex
	exits     map[uint32]chan int
	alivePIDs map[uint32]bool
}

func newMockWatcher() *mockWatcher {
	return &mockWatcher{
		exits:     make(map[uint32]chan int),
		alivePIDs: make(map[uint32]bool),
	}
}

func (m *mockWatcher) WaitForExit(ctx context.Context, pid uint32) (int, error) {
	m.mu.Lock()
	ch, ok := m.exits[pid]
	if !ok {
		ch = make(chan int, 1)
		m.exits[pid] = ch
	}
	m.alivePIDs[pid] = true
	m.mu.Unlock()

	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case code := <-ch:
		m.mu.Lock()
		delete(m.alivePIDs, pid)
		m.mu.Unlock()
		return code, nil
	}
}

func (m *mockWatcher) IsAlive(pid uint32) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.alivePIDs[pid]
}

func (m *mockWatcher) triggerExit(pid uint32, code int) {
	m.mu.Lock()
	ch, ok := m.exits[pid]
	if !ok {
		ch = make(chan int, 1)
		m.exits[pid] = ch
	}
	m.mu.Unlock()
	ch <- code
}

// mockLauncher implements ProcessLauncher for simulated restarts.
type mockLauncher struct {
	nextPID atomic.Uint32
	spawned []uint32
	mu      sync.Mutex
}

func newMockLauncher(startPID uint32) *mockLauncher {
	ml := &mockLauncher{}
	ml.nextPID.Store(startPID)
	return ml
}

func (l *mockLauncher) StartProcess(execPath string, args []string) (uint32, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	pid := l.nextPID.Add(1)
	l.spawned = append(l.spawned, pid)
	return pid, nil
}

// mockHardener tracks hardened PIDs.
type mockHardener struct {
	hardenedPIDs []uint32
	mu           sync.Mutex
}

func (h *mockHardener) HardenProcessDACL(pid uint32) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hardenedPIDs = append(h.hardenedPIDs, pid)
	return nil
}

func TestWatchdogNormalMonitoring(t *testing.T) {
	watcher := newMockWatcher()
	launcher := newMockLauncher(1000)
	hardener := &mockHardener{}

	target := WatchTarget{
		PID:      1001,
		ExecPath: "agent.exe",
		Args:     []string{"--mode", "student"},
		RestartPolicy: RestartPolicy{
			MaxCrashes:   3,
			CrashWindow:  10 * time.Second,
			RestartDelay: 10 * time.Millisecond,
		},
	}

	sup, err := NewSupervisor(target,
		WithWatcher(watcher),
		WithLauncher(launcher),
		WithHardener(hardener),
	)
	if err != nil {
		t.Fatalf("NewSupervisor failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sup.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Should be actively monitoring
	time.Sleep(20 * time.Millisecond)
	if state := sup.State(); state != StateMonitoring {
		t.Errorf("expected state %s, got %s", StateMonitoring, state)
	}

	if pid := sup.CurrentPID(); pid != 1001 {
		t.Errorf("expected current PID 1001, got %d", pid)
	}

	// Normal clean stop
	if err := sup.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if state := sup.State(); state != StateStopped {
		t.Errorf("expected state %s after stop, got %s", StateStopped, state)
	}
}

func TestWatchdogCrashSimulationAndAutoRestart(t *testing.T) {
	watcher := newMockWatcher()
	launcher := newMockLauncher(2000)
	hardener := &mockHardener{}

	restartedPIDs := make([]uint32, 0)
	crashedPIDs := make([]uint32, 0)
	var cbMu sync.Mutex

	target := WatchTarget{
		PID:      2001,
		ExecPath: "agent.exe",
		Args:     []string{"--worker"},
		RestartPolicy: RestartPolicy{
			MaxCrashes:   5,
			CrashWindow:  10 * time.Second,
			RestartDelay: 20 * time.Millisecond, // < 200ms
		},
	}

	sup, err := NewSupervisor(target,
		WithWatcher(watcher),
		WithLauncher(launcher),
		WithHardener(hardener),
		WithCallbacks(
			func(newPID uint32) {
				cbMu.Lock()
				restartedPIDs = append(restartedPIDs, newPID)
				cbMu.Unlock()
			},
			func(pid uint32, code int) {
				cbMu.Lock()
				crashedPIDs = append(crashedPIDs, pid)
				cbMu.Unlock()
			},
			nil,
		),
	)
	if err != nil {
		t.Fatalf("NewSupervisor failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sup.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait for supervisor to enter monitoring
	time.Sleep(30 * time.Millisecond)

	// Simulate crash of PID 2001
	startCrash := time.Now()
	watcher.triggerExit(2001, 1)

	// Wait for restart (must be < 200ms)
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		if sup.RestartCount() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	restartLatency := time.Since(startCrash)
	if restartLatency > 200*time.Millisecond {
		t.Errorf("restart latency took %v, expected < 200ms", restartLatency)
	}

	if count := sup.RestartCount(); count != 1 {
		t.Errorf("expected 1 restart, got %d", count)
	}

	cbMu.Lock()
	if len(crashedPIDs) != 1 || crashedPIDs[0] != 2001 {
		t.Errorf("expected crashed PID [2001], got %v", crashedPIDs)
	}
	if len(restartedPIDs) != 1 {
		t.Errorf("expected 1 restarted PID, got %v", restartedPIDs)
	}
	cbMu.Unlock()

	_ = sup.Stop()
}

func TestWatchdogAntiAvalancheRateLimiting(t *testing.T) {
	watcher := newMockWatcher()
	launcher := newMockLauncher(3000)
	hardener := &mockHardener{}

	rateLimitHit := atomic.Bool{}
	maxCrashes := 3

	target := WatchTarget{
		PID:      3001,
		ExecPath: "agent.exe",
		RestartPolicy: RestartPolicy{
			MaxCrashes:   maxCrashes,
			CrashWindow:  5 * time.Second,
			RestartDelay: 5 * time.Millisecond,
		},
	}

	sup, err := NewSupervisor(target,
		WithWatcher(watcher),
		WithLauncher(launcher),
		WithHardener(hardener),
		WithCallbacks(nil, nil, func(crashes int) {
			rateLimitHit.Store(true)
		}),
	)
	if err != nil {
		t.Fatalf("NewSupervisor failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sup.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	currentPID := uint32(3001)
	// Rapidly crash more than MaxCrashes
	for i := 0; i <= maxCrashes; i++ {
		time.Sleep(15 * time.Millisecond)
		watcher.triggerExit(currentPID, 1)

		// Wait briefly for restart or rate limit
		time.Sleep(20 * time.Millisecond)
		currentPID = sup.CurrentPID()
	}

	// Verify rate limiter engaged
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if sup.State() == StateRateLimited {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if state := sup.State(); state != StateRateLimited {
		t.Errorf("expected StateRateLimited, got %s", state)
	}
	if !rateLimitHit.Load() {
		t.Errorf("expected onRateLimit callback to have executed")
	}

	_ = sup.Stop()
}

func TestBlacklistKillerFilter(t *testing.T) {
	kf := NewKillerFilter("malicious-tool.exe", "killall.exe")

	tests := []struct {
		name     string
		procName string
		expected bool
	}{
		{"taskmgr lowercase", "taskmgr.exe", true},
		{"taskmgr uppercase", "TASKMGR.EXE", true},
		{"taskmgr without extension", "taskmgr", true},
		{"taskmgr with path", `C:\Windows\System32\Taskmgr.exe`, true},
		{"jiyutrainer exact", "jiyutrainer.exe", true},
		{"jiyutrainer mixed case", "JiYuTrainer.exe", true},
		{"pchunter lowercase", "pchunter.exe", true},
		{"pchunter32", "pchunter32.exe", true},
		{"pchunter64", "PCHunter64.exe", true},
		{"processhacker", "processhacker.exe", true},
		{"procexp", "procexp.exe", true},
		{"custom tool", "malicious-tool.exe", true},
		{"custom tool without exe", "malicious-tool", true},
		{"benign agent", "agent.exe", false},
		{"benign student app", "localmesh-agent.exe", false},
		{"benign notepad", "notepad.exe", false},
		{"benign explorer", "explorer.exe", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := kf.Matches(tc.procName)
			if actual != tc.expected {
				t.Errorf("Matches(%q) = %v; want %v", tc.procName, actual, tc.expected)
			}
		})
	}

	// Test global function
	if !IsBlacklistedKiller("taskmgr.exe") {
		t.Errorf("IsBlacklistedKiller('taskmgr.exe') expected true")
	}
	if !IsBlacklistedKiller("jiyutrainer.exe") {
		t.Errorf("IsBlacklistedKiller('jiyutrainer.exe') expected true")
	}
	if !IsBlacklistedKiller("pchunter.exe") {
		t.Errorf("IsBlacklistedKiller('pchunter.exe') expected true")
	}
	if IsBlacklistedKiller("cmd.exe") {
		t.Errorf("IsBlacklistedKiller('cmd.exe') expected false")
	}

	// Test Scan batch method
	procs := []string{"notepad.exe", "Taskmgr.exe", "chrome.exe", "JiYuTrainer.exe", "code.exe"}
	matched := kf.Scan(procs)
	if len(matched) != 2 {
		t.Fatalf("expected 2 matches from scan, got %d: %v", len(matched), matched)
	}
}

func TestWatchdogConcurrentStateAccess(t *testing.T) {
	watcher := newMockWatcher()
	launcher := newMockLauncher(4000)

	target := WatchTarget{
		PID:      4001,
		ExecPath: "agent.exe",
		RestartPolicy: RestartPolicy{
			MaxCrashes:   10,
			CrashWindow:  10 * time.Second,
			RestartDelay: 5 * time.Millisecond,
		},
	}

	sup, err := NewSupervisor(target, WithWatcher(watcher), WithLauncher(launcher))
	if err != nil {
		t.Fatalf("NewSupervisor failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sup.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	var wg sync.WaitGroup
	workers := 10
	iterations := 100

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = sup.State()
				_ = sup.CurrentPID()
				_ = sup.RestartCount()
				if j%25 == 0 {
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
	_ = sup.Stop()
}

func TestHardenProcessDACLCurrentProcess(t *testing.T) {
	// Tests calling HardenProcessDACL on the current process
	err := HardenProcessDACL(0)
	if err != nil {
		t.Fatalf("HardenProcessDACL(0) failed: %v", err)
	}
}
