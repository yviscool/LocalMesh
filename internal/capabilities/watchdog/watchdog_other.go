//go:build !windows

package watchdog

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// PlatformProcessWatcher provides fallback process observation on non-Windows platforms.
type PlatformProcessWatcher struct{}

// WaitForExit polls process liveness until exit or context cancellation.
func (w *PlatformProcessWatcher) WaitForExit(ctx context.Context, pid uint32) (int, error) {
	if pid == 0 {
		return 0, ErrInvalidTarget
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-ticker.C:
			if !w.IsAlive(pid) {
				return 0, nil
			}
		}
	}
}

// IsAlive checks if a process is still alive using signal 0.
func (w *PlatformProcessWatcher) IsAlive(pid uint32) bool {
	if pid == 0 {
		return false
	}
	proc, err := os.FindProcess(int(pid))
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// PlatformProcessLauncher launches new processes on non-Windows platforms.
type PlatformProcessLauncher struct{}

// StartProcess launches an executable with command arguments.
func (l *PlatformProcessLauncher) StartProcess(execPath string, args []string) (uint32, error) {
	cmd := exec.Command(execPath, args...)
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrProcessLaunchFailed, err)
	}
	return uint32(cmd.Process.Pid), nil
}

// PlatformProcessHardener is a no-op fallback on non-Windows platforms.
type PlatformProcessHardener struct{}

// HardenProcessDACL is a no-op on non-Windows platforms.
func HardenProcessDACL(pid uint32) error {
	return nil
}

func (h *PlatformProcessHardener) HardenProcessDACL(pid uint32) error {
	return HardenProcessDACL(pid)
}

// NewDefaultWatcher constructs the portable fallback process watcher.
func NewDefaultWatcher() ProcessWatcher {
	return &PlatformProcessWatcher{}
}

// NewDefaultLauncher constructs the portable process launcher.
func NewDefaultLauncher() ProcessLauncher {
	return &PlatformProcessLauncher{}
}

// NewDefaultHardener constructs the fallback process hardener.
func NewDefaultHardener() ProcessHardener {
	return &PlatformProcessHardener{}
}
