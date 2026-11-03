package restrictions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidPolicyMode = errors.New("invalid policy mode: must be blocklist or allowlist")
	ErrEmptyPolicy       = errors.New("process list cannot be empty")
	ErrProcessNotFound   = errors.New("process not found")
)

type PolicyMode string

const (
	PolicyBlocklist PolicyMode = "blocklist"
	PolicyAllowlist PolicyMode = "allowlist"
)

// ProcessPolicy defines restrictions for running applications.
type ProcessPolicy struct {
	Mode      PolicyMode `json:"mode"`
	Processes []string   `json:"processes"`
	Reason    string     `json:"reason,omitempty"`
}

// Validate checks policy integrity.
func (p ProcessPolicy) Validate() error {
	switch p.Mode {
	case PolicyBlocklist, PolicyAllowlist:
		if len(p.Processes) == 0 {
			return ErrEmptyPolicy
		}
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidPolicyMode, p.Mode)
	}
}

// ProcessInfo represents basic metadata for a running process.
type ProcessInfo struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
}

// ProcessController provides platform operations for enumerating and terminating processes.
type ProcessController interface {
	ListProcesses(ctx context.Context) ([]ProcessInfo, error)
	KillProcess(ctx context.Context, pid int) error
}

// AuditAction defines the restriction audit action type.
type AuditAction string

const (
	AuditProcessTerminated AuditAction = "process.terminated"
	AuditProcessError      AuditAction = "process.error"
	AuditUSBPolicyChanged  AuditAction = "usb.policy_changed"
)

// AuditEvent represents an auditable security event emitted by restrictions.
type AuditEvent struct {
	Timestamp   time.Time   `json:"timestamp"`
	Action      AuditAction `json:"action"`
	ProcessName string      `json:"process_name,omitempty"`
	PID         int         `json:"pid,omitempty"`
	PolicyMode  PolicyMode  `json:"policy_mode,omitempty"`
	Reason      string      `json:"reason,omitempty"`
	Error       string      `json:"error,omitempty"`
}

// AuditSink receives audit events for compliance and logging.
type AuditSink interface {
	Record(ctx context.Context, event AuditEvent) error
}

// IsProtectedProcess returns true if the process is critical to the operating system
// or LocalMesh and should never be terminated by an allowlist policy.
func IsProtectedProcess(pid int, name string) bool {
	if pid <= 4 {
		return true
	}
	clean := strings.ToLower(strings.TrimSpace(name))
	base := strings.TrimSuffix(clean, ".exe")
	switch base {
	case "system", "registry", "smss", "csrss", "wininit", "services", "lsass",
		"svchost", "fontdrvhost", "winlogon", "dwm", "explorer", "sihost", "taskhostw",
		"localmesh", "agent", "localmesh-agent", "init", "systemd", "launchd":
		return true
	default:
		return false
	}
}

// MatchesProcess checks if a process name matches a pattern (case-insensitive, optional .exe).
func MatchesProcess(procName, pattern string) bool {
	p := strings.ToLower(strings.TrimSpace(procName))
	pat := strings.ToLower(strings.TrimSpace(pattern))
	if p == pat {
		return true
	}
	pBase := strings.TrimSuffix(p, ".exe")
	patBase := strings.TrimSuffix(pat, ".exe")
	return pBase == patBase
}

// IsRestricted determines if a given process should be terminated under the policy.
func IsRestricted(proc ProcessInfo, policy ProcessPolicy) bool {
	if IsProtectedProcess(proc.PID, proc.Name) {
		return false
	}

	matched := false
	for _, pattern := range policy.Processes {
		if MatchesProcess(proc.Name, pattern) {
			matched = true
			break
		}
	}

	if policy.Mode == PolicyBlocklist {
		return matched
	}
	// Allowlist mode: anything not matched is restricted
	return !matched
}

// EnforcementResult records the action taken for a specific process during evaluation.
type EnforcementResult struct {
	Process    ProcessInfo
	Restricted bool
	Terminated bool
	Error      error
}

// ProcessMonitor evaluates running processes against policies and terminates unauthorized processes.
type ProcessMonitor struct {
	controller ProcessController
	audit      AuditSink
	clock      func() time.Time
}

// NewProcessMonitor creates a ProcessMonitor with the provided controller and audit sink.
func NewProcessMonitor(controller ProcessController, audit AuditSink) *ProcessMonitor {
	return &ProcessMonitor{
		controller: controller,
		audit:      audit,
		clock:      time.Now,
	}
}

// SetClock sets a custom clock provider (useful for testing).
func (m *ProcessMonitor) SetClock(clock func() time.Time) {
	m.clock = clock
}

// EvaluateAndEnforce scans running processes, evaluates them against the policy,
// terminates non-compliant processes safely, and emits audit records.
func (m *ProcessMonitor) EvaluateAndEnforce(ctx context.Context, policy ProcessPolicy) ([]EnforcementResult, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if m.controller == nil {
		return nil, errors.New("process controller not configured")
	}

	procs, err := m.controller.ListProcesses(ctx)
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}

	now := m.clock()
	results := make([]EnforcementResult, 0, len(procs))

	for _, proc := range procs {
		if ctx.Err() != nil {
			return results, ctx.Err()
		}

		restricted := IsRestricted(proc, policy)
		if !restricted {
			results = append(results, EnforcementResult{
				Process:    proc,
				Restricted: false,
			})
			continue
		}

		// Safe termination
		killErr := m.controller.KillProcess(ctx, proc.PID)
		res := EnforcementResult{
			Process:    proc,
			Restricted: true,
			Terminated: killErr == nil,
			Error:      killErr,
		}

		if killErr != nil {
			if m.audit != nil {
				_ = m.audit.Record(ctx, AuditEvent{
					Timestamp:   now,
					Action:      AuditProcessError,
					ProcessName: proc.Name,
					PID:         proc.PID,
					PolicyMode:  policy.Mode,
					Reason:      policy.Reason,
					Error:       killErr.Error(),
				})
			}
		} else {
			if m.audit != nil {
				_ = m.audit.Record(ctx, AuditEvent{
					Timestamp:   now,
					Action:      AuditProcessTerminated,
					ProcessName: proc.Name,
					PID:         proc.PID,
					PolicyMode:  policy.Mode,
					Reason:      policy.Reason,
				})
			}
		}

		results = append(results, res)
	}

	return results, nil
}

// OSProcessController enumerates and terminates local processes using the
// platform toolchain (tasklist/taskkill on Windows). ListProcesses and
// KillProcess live in the platform-specific oscontroller_*.go files.
type OSProcessController struct{}
