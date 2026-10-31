package power

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidAction      = errors.New("invalid power action")
	ErrInvalidGracePeriod = errors.New("grace period must be non-negative")
	ErrMissingMAC         = errors.New("MAC address is required for WOL")
	ErrInvalidPayload     = errors.New("invalid power payload")
	ErrUnsupportedAction  = errors.New("unsupported power action on this platform")
)

type PowerAction string

const (
	ActionShutdown PowerAction = "shutdown"
	ActionReboot   PowerAction = "reboot"
	ActionLogoff   PowerAction = "logoff"
	ActionLock     PowerAction = "lock"
	ActionWOL      PowerAction = "wol"
)

// PowerRequest defines the request payload for power management operations.
type PowerRequest struct {
	Action             PowerAction `json:"action"`
	GracePeriodSeconds int         `json:"grace_period_seconds,omitempty"`
	Message            string      `json:"message,omitempty"`
	Force              bool        `json:"force,omitempty"`
	MAC                string      `json:"mac,omitempty"`
	BroadcastAddr      string      `json:"broadcast_addr,omitempty"`
}

// Validate checks whether the power request payload is valid.
func (r PowerRequest) Validate() error {
	switch r.Action {
	case ActionShutdown, ActionReboot, ActionLogoff, ActionLock:
		if r.GracePeriodSeconds < 0 {
			return ErrInvalidGracePeriod
		}
		return nil
	case ActionWOL:
		if strings.TrimSpace(r.MAC) == "" {
			return ErrMissingMAC
		}
		mac, err := net.ParseMAC(r.MAC)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidMAC, err)
		}
		if len(mac) != macLen {
			return fmt.Errorf("%w: got %d bytes", ErrInvalidMAC, len(mac))
		}
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidAction, r.Action)
	}
}

// ParseRequest parses a JSON-encoded byte payload into a PowerRequest and validates it.
func ParseRequest(payload []byte) (PowerRequest, error) {
	if len(payload) == 0 {
		return PowerRequest{}, ErrInvalidPayload
	}
	var req PowerRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return PowerRequest{}, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}
	if err := req.Validate(); err != nil {
		return PowerRequest{}, err
	}
	return req, nil
}

// Executor defines the platform port for executing system power operations.
type Executor interface {
	Shutdown(ctx context.Context, force bool, gracePeriod time.Duration, msg string) error
	Reboot(ctx context.Context, force bool, gracePeriod time.Duration, msg string) error
	Logoff(ctx context.Context, force bool) error
	Lock(ctx context.Context) error
}

// WOLSender is the function signature for sending Wake-on-LAN packets.
type WOLSender func(mac net.HardwareAddr, broadcastAddr string) error

// Service coordinates power operations using the execution port and WOL sender.
type Service struct {
	executor  Executor
	wolSender WOLSender
}

// NewService creates a power Service with the provided executor and WOL sender.
// If wolSender is nil, SendWakeOnLAN is used by default.
func NewService(executor Executor, wolSender WOLSender) *Service {
	if wolSender == nil {
		wolSender = SendWakeOnLAN
	}
	return &Service{
		executor:  executor,
		wolSender: wolSender,
	}
}

// Execute processes and executes a power request.
func (s *Service) Execute(ctx context.Context, req PowerRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}

	grace := time.Duration(req.GracePeriodSeconds) * time.Second

	switch req.Action {
	case ActionShutdown:
		if s.executor == nil {
			return errors.New("power executor not configured")
		}
		return s.executor.Shutdown(ctx, req.Force, grace, req.Message)

	case ActionReboot:
		if s.executor == nil {
			return errors.New("power executor not configured")
		}
		return s.executor.Reboot(ctx, req.Force, grace, req.Message)

	case ActionLogoff:
		if s.executor == nil {
			return errors.New("power executor not configured")
		}
		return s.executor.Logoff(ctx, req.Force)

	case ActionLock:
		if s.executor == nil {
			return errors.New("power executor not configured")
		}
		return s.executor.Lock(ctx)

	case ActionWOL:
		mac, err := net.ParseMAC(req.MAC)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidMAC, err)
		}
		return s.wolSender(mac, req.BroadcastAddr)

	default:
		return fmt.Errorf("%w: %q", ErrInvalidAction, req.Action)
	}
}

// CommandRunner defines a function signature for running external commands.
type CommandRunner func(ctx context.Context, name string, args ...string) error

// OSCommandExecutor executes power commands via operating system utilities.
// Runner can be injected for testing.
type OSCommandExecutor struct {
	Runner CommandRunner
	OS     string // defaults to runtime.GOOS if empty
}

func (e *OSCommandExecutor) targetOS() string {
	if e.OS != "" {
		return e.OS
	}
	return runtime.GOOS
}

func (e *OSCommandExecutor) run(ctx context.Context, name string, args ...string) error {
	if e.Runner != nil {
		return e.Runner(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).Run()
}

// Shutdown initiates a system shutdown with the given options.
func (e *OSCommandExecutor) Shutdown(ctx context.Context, force bool, gracePeriod time.Duration, msg string) error {
	switch e.targetOS() {
	case "windows":
		args := []string{"/s", "/t", strconv.Itoa(int(gracePeriod.Seconds()))}
		if force {
			args = append(args, "/f")
		}
		if msg != "" {
			args = append(args, "/c", msg)
		}
		return e.run(ctx, "shutdown.exe", args...)

	case "linux":
		mins := int(gracePeriod.Minutes())
		timeArg := fmt.Sprintf("+%d", mins)
		if gracePeriod == 0 {
			timeArg = "now"
		}
		args := []string{"-h", timeArg}
		if msg != "" {
			args = append(args, msg)
		}
		return e.run(ctx, "shutdown", args...)

	case "darwin":
		mins := int(gracePeriod.Minutes())
		timeArg := fmt.Sprintf("+%d", mins)
		if gracePeriod == 0 {
			timeArg = "now"
		}
		args := []string{"-h", timeArg}
		if msg != "" {
			args = append(args, msg)
		}
		return e.run(ctx, "shutdown", args...)

	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedAction, e.targetOS())
	}
}

// Reboot initiates a system reboot with the given options.
func (e *OSCommandExecutor) Reboot(ctx context.Context, force bool, gracePeriod time.Duration, msg string) error {
	switch e.targetOS() {
	case "windows":
		args := []string{"/r", "/t", strconv.Itoa(int(gracePeriod.Seconds()))}
		if force {
			args = append(args, "/f")
		}
		if msg != "" {
			args = append(args, "/c", msg)
		}
		return e.run(ctx, "shutdown.exe", args...)

	case "linux":
		mins := int(gracePeriod.Minutes())
		timeArg := fmt.Sprintf("+%d", mins)
		if gracePeriod == 0 {
			timeArg = "now"
		}
		args := []string{"-r", timeArg}
		if msg != "" {
			args = append(args, msg)
		}
		return e.run(ctx, "shutdown", args...)

	case "darwin":
		mins := int(gracePeriod.Minutes())
		timeArg := fmt.Sprintf("+%d", mins)
		if gracePeriod == 0 {
			timeArg = "now"
		}
		args := []string{"-r", timeArg}
		if msg != "" {
			args = append(args, msg)
		}
		return e.run(ctx, "shutdown", args...)

	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedAction, e.targetOS())
	}
}

// Logoff signs out the current interactive user session.
func (e *OSCommandExecutor) Logoff(ctx context.Context, force bool) error {
	switch e.targetOS() {
	case "windows":
		args := []string{"/l"}
		if force {
			args = append(args, "/f")
		}
		return e.run(ctx, "shutdown.exe", args...)

	case "linux":
		return e.run(ctx, "loginctl", "terminate-user", "")

	case "darwin":
		return e.run(ctx, "osascript", "-e", "tell application \"loginwindow\" to  «event aevtrlgo»")

	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedAction, e.targetOS())
	}
}

// Lock locks the workstation screen.
func (e *OSCommandExecutor) Lock(ctx context.Context) error {
	switch e.targetOS() {
	case "windows":
		return e.run(ctx, "rundll32.exe", "user32.dll,LockWorkStation")

	case "linux":
		return e.run(ctx, "loginctl", "lock-session")

	case "darwin":
		return e.run(ctx, "pmset", "displaysleepnow")

	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedAction, e.targetOS())
	}
}
