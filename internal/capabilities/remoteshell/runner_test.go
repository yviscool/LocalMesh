package remoteshell

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockAuditSink struct {
	mu     sync.Mutex
	events []AuditEvent
}

func (m *mockAuditSink) Record(ctx context.Context, event AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *mockAuditSink) Events() []AuditEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]AuditEvent, len(m.events))
	copy(copied, m.events)
	return copied
}

func TestRunnerTableDrivenScenarios(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows-specific runner tests on non-Windows environment")
	}

	audit := &mockAuditSink{}
	runner := NewRunner(WithAuditSink(audit))

	tests := []struct {
		name             string
		req              ScriptExecutionRequest
		expectedExitCode int
		expectedStdout   string
		expectedStderr   string
		expectedTrunc    bool
		expectError      bool
		minDurationMs    int64
	}{
		{
			name: "Batch echo test",
			req: ScriptExecutionRequest{
				ScriptType:     ScriptTypeBatch,
				Content:        "@echo off\necho Hello from Batch",
				TimeoutSeconds: 5,
			},
			expectedExitCode: 0,
			expectedStdout:   "Hello from Batch",
			expectedTrunc:    false,
			expectError:      false,
		},
		{
			name: "PowerShell echo test",
			req: ScriptExecutionRequest{
				ScriptType:     ScriptTypePowerShell,
				Content:        "Write-Output 'Hello from PowerShell'",
				TimeoutSeconds: 25,
			},
			expectedExitCode: 0,
			expectedStdout:   "Hello from PowerShell",
			expectedTrunc:    false,
			expectError:      false,
		},
		{
			name: "Non-zero exit code return test",
			req: ScriptExecutionRequest{
				ScriptType:     ScriptTypeBatch,
				Content:        "exit 42",
				TimeoutSeconds: 5,
			},
			expectedExitCode: 42,
			expectError:      true,
		},
		{
			name: "PowerShell non-zero exit code test",
			req: ScriptExecutionRequest{
				ScriptType:     ScriptTypePowerShell,
				Content:        "exit 7",
				TimeoutSeconds: 25,
			},
			expectedExitCode: 7,
			expectError:      true,
		},
		{
			name: "Max output truncation test",
			req: ScriptExecutionRequest{
				ScriptType:     ScriptTypeBatch,
				Content:        "@echo off\nfor /L %%i in (1,1,200) do echo LongOutputLineNumber%%iHere",
				TimeoutSeconds: 5,
				MaxOutputBytes: 100,
			},
			expectedExitCode: 0,
			expectedTrunc:    true,
			expectError:      false,
		},
		{
			name: "Syntax error handling test",
			req: ScriptExecutionRequest{
				ScriptType:     ScriptTypePowerShell,
				Content:        "Get-InvalidCommand-ThatDoesNotExist123456",
				TimeoutSeconds: 25,
			},
			expectedExitCode: 1, // PowerShell exits 1 on unhandled cmdlet error
			expectError:      true,
		},
		{
			name: "Environment variables pass-through test",
			req: ScriptExecutionRequest{
				ScriptType:     ScriptTypeBatch,
				Content:        "@echo off\necho %LOCALMESH_VAR%",
				TimeoutSeconds: 5,
				Env: map[string]string{
					"LOCALMESH_VAR": "MeshActive123",
				},
			},
			expectedExitCode: 0,
			expectedStdout:   "MeshActive123",
			expectedTrunc:    false,
			expectError:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := runner.Execute(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("unexpected runner error: %v", err)
			}

			if res.ExitCode != tc.expectedExitCode {
				t.Errorf("ExitCode = %d; want %d (Error: %q, Stderr: %q)", res.ExitCode, tc.expectedExitCode, res.Error, res.Stderr)
			}

			if tc.expectedStdout != "" && !strings.Contains(strings.TrimSpace(res.Stdout), tc.expectedStdout) {
				t.Errorf("Stdout %q does not contain %q", res.Stdout, tc.expectedStdout)
			}

			if res.Truncated != tc.expectedTrunc {
				t.Errorf("Truncated = %v; want %v (len stdout: %d)", res.Truncated, tc.expectedTrunc, len(res.Stdout))
			}

			if tc.expectedTrunc && len(res.Stdout) > tc.req.MaxOutputBytes {
				t.Errorf("Truncated stdout len %d exceeds limit %d", len(res.Stdout), tc.req.MaxOutputBytes)
			}

			if tc.expectError && res.Error == "" {
				t.Errorf("expected res.Error to be populated for non-zero exit code")
			}
		})
	}
}

func TestRunnerTimeoutForceKill(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows-specific timeout test on non-Windows")
	}

	treeKillInvoked := false
	var killMu sync.Mutex

	runner := NewRunner(
		WithTreeKiller(func(pid int) error {
			killMu.Lock()
			treeKillInvoked = true
			killMu.Unlock()
			return defaultKillProcessTree(pid)
		}),
	)

	// Command will sleep 10 seconds, but timeout is set to 1 second
	req := ScriptExecutionRequest{
		ScriptType:     ScriptTypeBatch,
		Content:        "@echo off\nping 127.0.0.1 -n 10 >nul",
		TimeoutSeconds: 1,
	}

	start := time.Now()
	res, err := runner.Execute(context.Background(), req)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected Execute error: %v", err)
	}

	if res.ExitCode != -1 {
		t.Errorf("expected ExitCode -1 on timeout, got %d", res.ExitCode)
	}

	if !strings.Contains(res.Error, "deadline exceeded") && !strings.Contains(res.Error, "context") {
		t.Errorf("expected timeout error, got %q", res.Error)
	}

	if elapsed > 3*time.Second {
		t.Errorf("execution took %v; expected timeout within ~1-2s", elapsed)
	}

	killMu.Lock()
	invoked := treeKillInvoked
	killMu.Unlock()

	if !invoked {
		t.Errorf("expected tree killer to have been invoked on timeout")
	}
}

func TestRunnerAuditLogging(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping audit test on non-Windows")
	}

	audit := &mockAuditSink{}
	runner := NewRunner(WithAuditSink(audit))

	req := ScriptExecutionRequest{
		ScriptType:     ScriptTypeBatch,
		Content:        "@echo off\necho AuditLogTest",
		TimeoutSeconds: 5,
	}

	res, err := runner.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected ExitCode 0, got %d", res.ExitCode)
	}

	events := audit.Events()
	if len(events) == 0 {
		t.Fatalf("expected audit event to be recorded")
	}

	last := events[len(events)-1]
	if last.Action != AuditScriptExecuted {
		t.Errorf("expected action %s, got %s", AuditScriptExecuted, last.Action)
	}
	if last.ScriptType != ScriptTypeBatch {
		t.Errorf("expected script type %s, got %s", ScriptTypeBatch, last.ScriptType)
	}
	if last.ContentHash == "" {
		t.Errorf("expected ContentHash to be populated")
	}
}

func TestScriptExecutionRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     ScriptExecutionRequest
		wantErr bool
	}{
		{
			name: "Valid batch request",
			req: ScriptExecutionRequest{
				ScriptType:     ScriptTypeBatch,
				Content:        "echo 1",
				TimeoutSeconds: 10,
			},
			wantErr: false,
		},
		{
			name: "Empty content",
			req: ScriptExecutionRequest{
				ScriptType:     ScriptTypeBatch,
				Content:        "   ",
				TimeoutSeconds: 10,
			},
			wantErr: true,
		},
		{
			name: "Unsupported script type",
			req: ScriptExecutionRequest{
				ScriptType:     ScriptType("python"),
				Content:        "print('hello')",
				TimeoutSeconds: 10,
			},
			wantErr: true,
		},
		{
			name: "Negative timeout",
			req: ScriptExecutionRequest{
				ScriptType:     ScriptTypePowerShell,
				Content:        "Write-Host 1",
				TimeoutSeconds: -5,
			},
			wantErr: true,
		},
		{
			name: "Non-existent working directory",
			req: ScriptExecutionRequest{
				ScriptType:     ScriptTypeBatch,
				Content:        "echo 1",
				TimeoutSeconds: 10,
				WorkingDir:     "C:\\ThisDirectoryShouldNotExist12345XYZ",
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v; wantErr %v", err, tc.wantErr)
			}
		})
	}
}
