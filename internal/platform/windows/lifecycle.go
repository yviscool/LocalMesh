//go:build windows

package windows

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	// ErrAlreadyRunning is returned when a single-instance mutex is already held.
	ErrAlreadyRunning = errors.New("instance already running")

	// Registry paths for Windows autostart configuration.
	runKeyPath      = `Software\Microsoft\Windows\CurrentVersion\Run`
	approvedKeyPath = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
)

// EnsureSingleInstance attempts to acquire an exclusive named mutex.
// If another instance holds the mutex, it returns ErrAlreadyRunning.
// On success, it returns a release function that must be called on shutdown.
func EnsureSingleInstance(mutexName string) (func(), error) {
	if strings.TrimSpace(mutexName) == "" {
		return nil, errors.New("mutexName cannot be empty")
	}

	namePtr, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return nil, fmt.Errorf("invalid mutex name %q: %w", mutexName, err)
	}

	handle, err := windows.CreateMutex(nil, true, namePtr)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if handle != 0 && handle != windows.InvalidHandle {
			_ = windows.CloseHandle(handle)
		}
		return nil, ErrAlreadyRunning
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create mutex %q: %w", mutexName, err)
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			_ = windows.ReleaseMutex(handle)
			_ = windows.CloseHandle(handle)
		})
	}

	return release, nil
}

// SetAutostart configures whether the specified application starts at user logon
// using HKCU\Software\Microsoft\Windows\CurrentVersion\Run.
func SetAutostart(appName, execPath string, enabled bool) error {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return errors.New("appName cannot be empty")
	}

	if enabled {
		execPath = strings.TrimSpace(execPath)
		if execPath == "" {
			return errors.New("execPath cannot be empty when enabled is true")
		}

		k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("failed to open run registry key: %w", err)
		}
		defer k.Close()

		val := execPath
		if !strings.HasPrefix(val, `"`) || !strings.HasSuffix(val, `"`) {
			val = `"` + strings.Trim(execPath, `"`) + `"`
		}

		if err := k.SetStringValue(appName, val); err != nil {
			return fmt.Errorf("failed to write run registry value: %w", err)
		}

		// Clear any Task Manager StartupApproved disabled marker
		ak, err := registry.OpenKey(registry.CURRENT_USER, approvedKeyPath, registry.SET_VALUE)
		if err == nil {
			defer ak.Close()
			_ = ak.DeleteValue(appName)
		}
		return nil
	}

	// Disable autostart
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to open run registry key: %w", err)
	}
	defer k.Close()

	if err := k.DeleteValue(appName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("failed to delete run registry value: %w", err)
	}
	return nil
}

// IsAutostartEnabled reports whether the application is configured to start at logon
// and is not disabled by Task Manager's Startup apps settings.
func IsAutostartEnabled(appName string) (bool, error) {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return false, errors.New("appName cannot be empty")
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("failed to open run registry key: %w", err)
	}
	defer k.Close()

	val, _, err := k.GetStringValue(appName)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("failed to query run registry value: %w", err)
	}
	if val == "" {
		return false, nil
	}

	// Check Task Manager StartupApproved key
	ak, err := registry.OpenKey(registry.CURRENT_USER, approvedKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return true, nil
	}
	defer ak.Close()

	b, _, err := ak.GetBinaryValue(appName)
	if err != nil {
		return true, nil
	}
	// Odd first byte indicates disabled in Task Manager
	if len(b) > 0 && (b[0]&1 == 1) {
		return false, nil
	}

	return true, nil
}
