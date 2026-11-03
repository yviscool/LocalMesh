//go:build !windows

package main

// ForegroundProcess is unsupported on non-Windows platforms.
func ForegroundProcess() (string, bool) {
	return "", false
}
