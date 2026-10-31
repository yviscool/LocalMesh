//go:build !windows

package windows

import (
	"errors"

	"localmesh/internal/platform"
)

var (
	// ErrAlreadyRunning is returned when a single-instance mutex is already held.
	ErrAlreadyRunning = errors.New("instance already running")
)

// EnsureSingleInstance returns ErrUnsupportedPlatform on non-Windows systems.
func EnsureSingleInstance(string) (func(), error) {
	return nil, platform.ErrUnsupportedPlatform
}

// SetAutostart returns ErrUnsupportedPlatform on non-Windows systems.
func SetAutostart(string, string, bool) error {
	return platform.ErrUnsupportedPlatform
}

// IsAutostartEnabled returns ErrUnsupportedPlatform on non-Windows systems.
func IsAutostartEnabled(string) (bool, error) {
	return false, platform.ErrUnsupportedPlatform
}
