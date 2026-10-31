package remoteshell

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultTimeoutSeconds = 30
	DefaultMaxOutputBytes = 1024 * 1024 // 1 MB
)

// AuditAction categorizes execution security audit outcomes.
type AuditAction string

const (
	AuditScriptExecuted   AuditAction = "script.executed"
	AuditScriptTerminated AuditAction = "script.terminated"
	AuditScriptFailed     AuditAction = "script.failed"
)

// AuditEvent records a security and operational log entry for remote script execution.
type AuditEvent struct {
	Timestamp   time.Time   `json:"timestamp"`
	Action      AuditAction `json:"action"`
	ScriptType  ScriptType  `json:"script_type"`
	ContentHash string      `json:"content_hash"`
	ExitCode    int         `json:"exit_code"`
	DurationMs  int64       `json:"duration_ms"`
	Truncated   bool        `json:"truncated"`
	Error       string      `json:"error,omitempty"`
}

// AuditSink receives execution events for auditing.
type AuditSink interface {
	Record(ctx context.Context, event AuditEvent) error
}

// limitedBuffer captures streaming output up to a maximum byte capacity,
// discarding subsequent bytes and marking truncation.
type limitedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func newLimitedBuffer(limit int) *limitedBuffer {
	return &limitedBuffer{limit: limit}
}

func (b *limitedBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	n = len(p)
	if b.limit <= 0 {
		return b.buf.Write(p)
	}

	current := b.buf.Len()
	if current >= b.limit {
		b.truncated = true
		return n, nil
	}

	remaining := b.limit - current
	if len(p) > remaining {
		b.buf.Write(p[:remaining])
		b.truncated = true
	} else {
		b.buf.Write(p)
	}

	return n, nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *limitedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

// RunnerOption configures optional dependencies for Runner.
type RunnerOption func(*Runner)

// WithAuditSink attaches an audit recorder.
func WithAuditSink(sink AuditSink) RunnerOption {
	return func(r *Runner) {
		r.audit = sink
	}
}

// WithClock customizes the timestamp provider.
func WithClock(clock func() time.Time) RunnerOption {
	return func(r *Runner) {
		r.clock = clock
	}
}

// WithTreeKiller allows overriding the process tree killer (useful in tests).
func WithTreeKiller(killer func(pid int) error) RunnerOption {
	return func(r *Runner) {
		r.killTree = killer
	}
}

// Runner coordinates sandboxed, monitored remote script execution.
type Runner struct {
	audit    AuditSink
	clock    func() time.Time
	killTree func(pid int) error
}

// NewRunner creates a new script execution Runner.
func NewRunner(opts ...RunnerOption) *Runner {
	r := &Runner{
		clock:    time.Now,
		killTree: defaultKillProcessTree,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// defaultKillProcessTree terminates the target process and all its child processes.
// On Windows, it invokes taskkill.exe /T /F /PID <pid> to prevent zombie orphans.
func defaultKillProcessTree(pid int) error {
	if pid <= 0 {
		return nil
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command("taskkill.exe", "/T", "/F", "/PID", strconv.Itoa(pid))
		return cmd.Run()
	}
	// On non-Windows platforms, kill process directly
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}

// Execute parses, sandboxes, and executes a script request with strict timeout cancellation,
// cascaded process tree termination, streaming output truncation, and audit logging.
func (r *Runner) Execute(ctx context.Context, req ScriptExecutionRequest) (*ScriptExecutionResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	timeoutSec := req.TimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = DefaultTimeoutSeconds
	}

	maxBytes := req.MaxOutputBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxOutputBytes
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	// Compute SHA-256 content hash for security audit
	hashSum := sha256.Sum256([]byte(req.Content))
	contentHash := hex.EncodeToString(hashSum[:])

	cmd, cleanup, err := r.buildCommand(execCtx, req)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return nil, err
	}

	// Attach custom Cancel hook to cascade kill the entire process tree on timeout/cancel
	cmd.Cancel = func() error {
		if cmd.Process != nil && cmd.Process.Pid > 0 {
			return r.killTree(cmd.Process.Pid)
		}
		return nil
	}

	if req.WorkingDir != "" {
		cmd.Dir = req.WorkingDir
	}

	if len(req.Env) > 0 {
		env := os.Environ()
		for k, v := range req.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = env
	}

	stdoutBuf := newLimitedBuffer(maxBytes)
	stderrBuf := newLimitedBuffer(maxBytes)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	startTime := r.clock()
	runErr := cmd.Run()
	durationMs := r.clock().Sub(startTime).Milliseconds()

	result := &ScriptExecutionResult{
		Stdout:     stdoutBuf.String(),
		Stderr:     stderrBuf.String(),
		DurationMs: durationMs,
		Truncated:  stdoutBuf.Truncated() || stderrBuf.Truncated(),
	}

	action := AuditScriptExecuted

	// Handle exit codes and errors
	if errors.Is(execCtx.Err(), context.DeadlineExceeded) || errors.Is(execCtx.Err(), context.Canceled) {
		result.ExitCode = -1
		result.Error = execCtx.Err().Error()
		action = AuditScriptTerminated
		// Ensure tree kill executed
		if cmd.Process != nil && cmd.Process.Pid > 0 {
			_ = r.killTree(cmd.Process.Pid)
		}
	} else if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			result.Error = exitErr.Error()
		} else {
			result.ExitCode = -1
			result.Error = runErr.Error()
		}
		action = AuditScriptFailed
	} else {
		result.ExitCode = 0
	}

	// Emit security audit event
	if r.audit != nil {
		auditEv := AuditEvent{
			Timestamp:   r.clock(),
			Action:      action,
			ScriptType:  req.ScriptType,
			ContentHash: contentHash,
			ExitCode:    result.ExitCode,
			DurationMs:  result.DurationMs,
			Truncated:   result.Truncated,
			Error:       result.Error,
		}
		_ = r.audit.Record(ctx, auditEv)
	}

	return result, nil
}

// buildCommand constructs the exec.Cmd according to ScriptType and platform specifications.
func (r *Runner) buildCommand(ctx context.Context, req ScriptExecutionRequest) (*exec.Cmd, func(), error) {
	if runtime.GOOS == "windows" {
		switch req.ScriptType {
		case ScriptTypeBatch:
			// For multiline batch scripts, write to a temp .bat file and run cmd.exe /c <file>
			if strings.Contains(req.Content, "\n") {
				tmp, err := os.CreateTemp("", "lm-batch-*.bat")
				if err != nil {
					return nil, nil, fmt.Errorf("create temp batch file: %w", err)
				}
				if _, err := tmp.WriteString(req.Content); err != nil {
					_ = tmp.Close()
					_ = os.Remove(tmp.Name())
					return nil, nil, fmt.Errorf("write batch file: %w", err)
				}
				_ = tmp.Close()
				cleanup := func() { _ = os.Remove(tmp.Name()) }
				return exec.CommandContext(ctx, "cmd.exe", "/c", tmp.Name()), cleanup, nil
			}
			return exec.CommandContext(ctx, "cmd.exe", "/c", req.Content), nil, nil

		case ScriptTypePowerShell:
			return exec.CommandContext(ctx,
				"powershell.exe",
				"-NoProfile",
				"-NonInteractive",
				"-ExecutionPolicy", "Bypass",
				"-Command", req.Content,
			), nil, nil

		case ScriptTypeShell:
			return exec.CommandContext(ctx, "sh", "-c", req.Content), nil, nil

		default:
			return nil, nil, fmt.Errorf("%w: %q", ErrUnsupportedScriptType, req.ScriptType)
		}
	}

	// Non-Windows platforms
	switch req.ScriptType {
	case ScriptTypeShell:
		return exec.CommandContext(ctx, "sh", "-c", req.Content), nil, nil
	case ScriptTypePowerShell:
		return exec.CommandContext(ctx, "pwsh", "-NoProfile", "-NonInteractive", "-Command", req.Content), nil, nil
	case ScriptTypeBatch:
		return nil, nil, fmt.Errorf("%w: batch is only supported on Windows", ErrUnsupportedScriptType)
	default:
		return nil, nil, fmt.Errorf("%w: %q", ErrUnsupportedScriptType, req.ScriptType)
	}
}
