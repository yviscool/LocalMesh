//go:build windows

package watchdog

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"golang.org/x/sys/windows"
)

const (
	// stillActive represents the Windows STATUS_PENDING / STILL_ACTIVE exit code.
	stillActive = 259

	// SDDL process security descriptor:
	// D:P - Protected DACL (disables ACE inheritance)
	// (D;;0x0801;;;WD) - Deny PROCESS_TERMINATE (0x0001) and PROCESS_SUSPEND_RESUME (0x0800) to World/Everyone (WD)
	// (A;;GA;;;SY)     - Allow Generic All to Local System (SY)
	// (A;;GA;;;BA)     - Allow Generic All to Builtin Administrators (BA)
	// (A;;0x101000;;;WD) - Allow SYNCHRONIZE (0x100000) and PROCESS_QUERY_LIMITED_INFORMATION (0x1000) to Everyone
	hardenProcessSDDL = "D:P(D;;0x0801;;;WD)(A;;GA;;;SY)(A;;GA;;;BA)(A;;0x101000;;;WD)"
)

// PlatformProcessWatcher uses native Win32 handles to observe process lifecycles.
type PlatformProcessWatcher struct{}

// WaitForExit waits for target process termination via OpenProcess and WaitForSingleObject.
// It detects termination within milliseconds and respects context cancellation.
func (w *PlatformProcessWatcher) WaitForExit(ctx context.Context, pid uint32) (int, error) {
	if pid == 0 {
		return 0, ErrInvalidTarget
	}

	// Open process with SYNCHRONIZE and query permissions
	access := uint32(windows.SYNCHRONIZE | windows.PROCESS_QUERY_LIMITED_INFORMATION)
	handle, err := windows.OpenProcess(access, false, pid)
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) || errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return 0, fmt.Errorf("%w: pid %d: %v", ErrProcessNotFound, pid, err)
		}
		return 0, fmt.Errorf("open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(handle)

	// Polling loop with 50ms wait slices to allow prompt context cancellation
	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		event, err := windows.WaitForSingleObject(handle, 50)
		if event == windows.WAIT_OBJECT_0 {
			var exitCode uint32
			if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
				return 0, fmt.Errorf("get exit code for pid %d: %w", pid, err)
			}
			return int(exitCode), nil
		}
		if event == windows.WAIT_FAILED {
			return 0, fmt.Errorf("wait for single object failed for pid %d: %w", pid, err)
		}
	}
}

// IsAlive checks whether a process is actively running.
func (w *PlatformProcessWatcher) IsAlive(pid uint32) bool {
	if pid == 0 {
		return false
	}
	access := uint32(windows.PROCESS_QUERY_LIMITED_INFORMATION)
	handle, err := windows.OpenProcess(access, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)

	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false
	}
	return exitCode == stillActive
}

// PlatformProcessLauncher launches new processes on Windows.
type PlatformProcessLauncher struct{}

// StartProcess launches an executable with command arguments and returns its PID.
func (l *PlatformProcessLauncher) StartProcess(execPath string, args []string) (uint32, error) {
	cmd := exec.Command(execPath, args...)
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrProcessLaunchFailed, err)
	}
	return uint32(cmd.Process.Pid), nil
}

// PlatformProcessHardener reinforces process DACL security attributes.
type PlatformProcessHardener struct{}

// HardenProcessDACL enforces DACL protection on the target process object,
// denying PROCESS_TERMINATE and PROCESS_SUSPEND_RESUME from non-elevated users.
// If pid is 0, it hardens the current calling process.
func HardenProcessDACL(pid uint32) error {
	var handle windows.Handle
	var err error

	if pid == 0 || pid == uint32(windows.GetCurrentProcessId()) {
		handle = windows.CurrentProcess()
	} else {
		handle, err = windows.OpenProcess(windows.WRITE_DAC|windows.READ_CONTROL, false, pid)
		if err != nil {
			return fmt.Errorf("open process for DACL hardening: %w", err)
		}
		defer windows.CloseHandle(handle)
	}

	sd, err := windows.SecurityDescriptorFromString(hardenProcessSDDL)
	if err != nil {
		return fmt.Errorf("create security descriptor from SDDL: %w", err)
	}

	if err := windows.SetKernelObjectSecurity(handle, windows.DACL_SECURITY_INFORMATION, sd); err != nil {
		return fmt.Errorf("set kernel object security: %w", err)
	}

	return nil
}

func (h *PlatformProcessHardener) HardenProcessDACL(pid uint32) error {
	return HardenProcessDACL(pid)
}

// NewDefaultWatcher constructs the Windows Win32 native process watcher.
func NewDefaultWatcher() ProcessWatcher {
	return &PlatformProcessWatcher{}
}

// NewDefaultLauncher constructs the Windows native process launcher.
func NewDefaultLauncher() ProcessLauncher {
	return &PlatformProcessLauncher{}
}

// NewDefaultHardener constructs the Windows process DACL hardener.
func NewDefaultHardener() ProcessHardener {
	return &PlatformProcessHardener{}
}
