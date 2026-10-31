package restrictions

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

type mockProcessController struct {
	mu          sync.Mutex
	processes   []ProcessInfo
	killedPIDs  []int
	killErrFunc func(pid int) error
}

func (m *mockProcessController) ListProcesses(_ context.Context) ([]ProcessInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ProcessInfo, len(m.processes))
	copy(out, m.processes)
	return out, nil
}

func (m *mockProcessController) KillProcess(_ context.Context, pid int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.killedPIDs = append(m.killedPIDs, pid)
	if m.killErrFunc != nil {
		return m.killErrFunc(pid)
	}
	return nil
}

type mockAuditSink struct {
	mu     sync.Mutex
	events []AuditEvent
}

func (m *mockAuditSink) Record(_ context.Context, event AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *mockAuditSink) Events() []AuditEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]AuditEvent, len(m.events))
	copy(out, m.events)
	return out
}

func TestProcessPolicyValidation(t *testing.T) {
	tests := []struct {
		name      string
		policy    ProcessPolicy
		expectErr error
	}{
		{
			name: "valid blocklist",
			policy: ProcessPolicy{
				Mode:      PolicyBlocklist,
				Processes: []string{"game.exe", "taskmgr.exe"},
			},
			expectErr: nil,
		},
		{
			name: "valid allowlist",
			policy: ProcessPolicy{
				Mode:      PolicyAllowlist,
				Processes: []string{"calc.exe"},
			},
			expectErr: nil,
		},
		{
			name: "empty processes",
			policy: ProcessPolicy{
				Mode:      PolicyBlocklist,
				Processes: nil,
			},
			expectErr: ErrEmptyPolicy,
		},
		{
			name: "invalid mode",
			policy: ProcessPolicy{
				Mode:      "random",
				Processes: []string{"something"},
			},
			expectErr: ErrInvalidPolicyMode,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.policy.Validate()
			if tc.expectErr != nil {
				if !errors.Is(err, tc.expectErr) {
					t.Fatalf("expected error %v, got %v", tc.expectErr, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestProcessMatchingAndRestrictions(t *testing.T) {
	t.Run("blocklist case-insensitivity and extensions", func(t *testing.T) {
		policy := ProcessPolicy{
			Mode:      PolicyBlocklist,
			Processes: []string{"TaskMgr.exe", "GAME"},
		}

		tests := []struct {
			proc       ProcessInfo
			restricted bool
		}{
			{ProcessInfo{PID: 100, Name: "taskmgr.exe"}, true},
			{ProcessInfo{PID: 101, Name: "TASKMGR.EXE"}, true},
			{ProcessInfo{PID: 102, Name: "taskmgr"}, true},
			{ProcessInfo{PID: 103, Name: "game.exe"}, true},
			{ProcessInfo{PID: 104, Name: "Game"}, true},
			{ProcessInfo{PID: 105, Name: "notepad.exe"}, false},
			{ProcessInfo{PID: 4, Name: "System"}, false}, // PID <= 4 protected
		}

		for _, tc := range tests {
			res := IsRestricted(tc.proc, policy)
			if res != tc.restricted {
				t.Fatalf("proc %+v: expected restricted=%v, got %v", tc.proc, tc.restricted, res)
			}
		}
	})

	t.Run("allowlist preserves protected processes", func(t *testing.T) {
		policy := ProcessPolicy{
			Mode:      PolicyAllowlist,
			Processes: []string{"python.exe", "vscode.exe"},
		}

		tests := []struct {
			proc       ProcessInfo
			restricted bool
		}{
			{ProcessInfo{PID: 200, Name: "python.exe"}, false},    // allowed
			{ProcessInfo{PID: 201, Name: "PYTHON"}, false},        // allowed case-insensitive
			{ProcessInfo{PID: 202, Name: "game.exe"}, true},       // not in allowlist -> restricted
			{ProcessInfo{PID: 4, Name: "System"}, false},          // protected system
			{ProcessInfo{PID: 500, Name: "explorer.exe"}, false},  // protected system shell
			{ProcessInfo{PID: 600, Name: "localmesh.exe"}, false}, // protected agent
		}

		for _, tc := range tests {
			res := IsRestricted(tc.proc, policy)
			if res != tc.restricted {
				t.Fatalf("proc %+v: expected restricted=%v, got %v", tc.proc, tc.restricted, res)
			}
		}
	})
}

func TestProcessMonitorEvaluateAndEnforce(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

	procs := []ProcessInfo{
		{PID: 4, Name: "System"},
		{PID: 100, Name: "taskmgr.exe"},
		{PID: 200, Name: "notepad.exe"},
		{PID: 300, Name: "game.exe"},
		{PID: 400, Name: "unstable_game.exe"},
	}

	ctrl := &mockProcessController{
		processes: procs,
		killErrFunc: func(pid int) error {
			if pid == 400 {
				return errors.New("access denied: process already exited")
			}
			return nil
		},
	}

	audit := &mockAuditSink{}
	monitor := NewProcessMonitor(ctrl, audit)
	monitor.SetClock(func() time.Time { return now })

	policy := ProcessPolicy{
		Mode:      PolicyBlocklist,
		Processes: []string{"taskmgr.exe", "game.exe", "unstable_game.exe"},
		Reason:    "Exam in progress",
	}

	results, err := monitor.EvaluateAndEnforce(ctx, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != len(procs) {
		t.Fatalf("expected %d results, got %d", len(procs), len(results))
	}

	// Verify killed PIDs: 100, 300, 400
	expectedKills := []int{100, 300, 400}
	if !reflect.DeepEqual(ctrl.killedPIDs, expectedKills) {
		t.Fatalf("expected killed PIDs %v, got %v", expectedKills, ctrl.killedPIDs)
	}

	// Verify PID 4 (System) and PID 200 (notepad) were not restricted
	if results[0].Restricted || results[2].Restricted {
		t.Fatal("System and Notepad should not be restricted")
	}

	// Verify PID 100 and 300 succeeded
	if !results[1].Terminated || results[1].Error != nil {
		t.Fatalf("PID 100 should be terminated cleanly: %+v", results[1])
	}
	if !results[3].Terminated || results[3].Error != nil {
		t.Fatalf("PID 300 should be terminated cleanly: %+v", results[3])
	}

	// Verify PID 400 recorded error safely without breaking execution
	if results[4].Terminated || results[4].Error == nil {
		t.Fatalf("PID 400 should have termination error: %+v", results[4])
	}

	// Verify Audit Events
	events := audit.Events()
	if len(events) != 3 {
		t.Fatalf("expected 3 audit events, got %d", len(events))
	}

	// Events 0 and 1 should be AuditProcessTerminated
	if events[0].Action != AuditProcessTerminated || events[0].PID != 100 || events[0].ProcessName != "taskmgr.exe" {
		t.Fatalf("unexpected event 0: %+v", events[0])
	}
	if events[1].Action != AuditProcessTerminated || events[1].PID != 300 || events[1].ProcessName != "game.exe" {
		t.Fatalf("unexpected event 1: %+v", events[1])
	}

	// Event 2 should be AuditProcessError
	if events[2].Action != AuditProcessError || events[2].PID != 400 || events[2].Error == "" {
		t.Fatalf("unexpected event 2: %+v", events[2])
	}
}

type mockRegistryOperator struct {
	mu     sync.Mutex
	values map[string]uint32
}

func newMockRegistryOperator() *mockRegistryOperator {
	return &mockRegistryOperator{values: make(map[string]uint32)}
}

func (m *mockRegistryOperator) GetDWORD(path, name string) (uint32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := path + "\\" + name
	val, ok := m.values[key]
	if !ok {
		return 0, errors.New("registry key not found")
	}
	return val, nil
}

func (m *mockRegistryOperator) SetDWORD(path, name string, val uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := path + "\\" + name
	m.values[key] = val
	return nil
}

func TestUSBPolicyAndPort(t *testing.T) {
	ctx := context.Background()

	t.Run("USB policy validation", func(t *testing.T) {
		tests := []struct {
			mode      USBPolicyMode
			expectErr bool
		}{
			{USBAllowAll, false},
			{USBReadOnly, false},
			{USBDisabled, false},
			{"unsupported", true},
		}

		for _, tc := range tests {
			p := USBPolicy{Mode: tc.mode}
			err := p.Validate()
			if (err != nil) != tc.expectErr {
				t.Fatalf("mode %s: expected err=%v, got %v", tc.mode, tc.expectErr, err)
			}
		}
	})

	t.Run("WindowsRegistryUSBPort operations", func(t *testing.T) {
		reg := newMockRegistryOperator()
		port := &WindowsRegistryUSBPort{Registry: reg}

		// Apply Disabled
		err := port.ApplyPolicy(ctx, USBPolicy{Mode: USBDisabled})
		if err != nil {
			t.Fatalf("apply USBDisabled: %v", err)
		}
		cur, err := port.CurrentPolicy(ctx)
		if err != nil {
			t.Fatalf("current policy: %v", err)
		}
		if cur.Mode != USBDisabled {
			t.Fatalf("expected USBDisabled, got %s", cur.Mode)
		}

		// Apply ReadOnly
		err = port.ApplyPolicy(ctx, USBPolicy{Mode: USBReadOnly})
		if err != nil {
			t.Fatalf("apply USBReadOnly: %v", err)
		}
		cur, err = port.CurrentPolicy(ctx)
		if err != nil {
			t.Fatalf("current policy: %v", err)
		}
		if cur.Mode != USBReadOnly {
			t.Fatalf("expected USBReadOnly, got %s", cur.Mode)
		}

		// Apply AllowAll
		err = port.ApplyPolicy(ctx, USBPolicy{Mode: USBAllowAll})
		if err != nil {
			t.Fatalf("apply USBAllowAll: %v", err)
		}
		cur, err = port.CurrentPolicy(ctx)
		if err != nil {
			t.Fatalf("current policy: %v", err)
		}
		if cur.Mode != USBAllowAll {
			t.Fatalf("expected USBAllowAll, got %s", cur.Mode)
		}
	})

	t.Run("USBManager with audit logging", func(t *testing.T) {
		reg := newMockRegistryOperator()
		port := &WindowsRegistryUSBPort{Registry: reg}
		audit := &mockAuditSink{}
		fixedTime := time.Date(2026, 10, 31, 14, 0, 0, 0, time.UTC)

		mgr := NewUSBManager(port, audit)
		mgr.SetClock(func() time.Time { return fixedTime })

		policy := USBPolicy{
			Mode:   USBReadOnly,
			Reason: "Prevent student data exfiltration",
		}

		if err := mgr.Apply(ctx, policy); err != nil {
			t.Fatalf("mgr.Apply failed: %v", err)
		}

		cur, err := mgr.Current(ctx)
		if err != nil {
			t.Fatalf("mgr.Current failed: %v", err)
		}
		if cur.Mode != USBReadOnly {
			t.Fatalf("expected USBReadOnly, got %s", cur.Mode)
		}

		events := audit.Events()
		if len(events) != 1 {
			t.Fatalf("expected 1 audit event, got %d", len(events))
		}
		ev := events[0]
		if ev.Action != AuditUSBPolicyChanged || ev.PolicyMode != "read_only" || ev.Reason != "Prevent student data exfiltration" {
			t.Fatalf("unexpected audit event: %+v", ev)
		}
	})
}
