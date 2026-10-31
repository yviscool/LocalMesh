//go:build !windows

package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
)

const (
	ServiceName         = "LocalMeshAgent"
	ServiceDisplayName  = "LocalMesh Student Agent"
	ServiceDescription  = "LocalMesh background student agent and security watchdog service"
	DefaultExitPassword = "LocalMeshAdmin123!"
)

func IsWindowsService() bool {
	return false
}

func RunService(startApp func(ctx context.Context) error, stopApp func()) error {
	return errors.New("windows service is only supported on windows")
}

func InstallService(execPath string, args []string) error {
	return errors.New("windows service is only supported on windows")
}

func UninstallService() error {
	return errors.New("windows service is only supported on windows")
}

func HashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

func VerifyExitPassword(candidate, expectedHashOrPlain string) bool {
	if candidate == "" || expectedHashOrPlain == "" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(candidate), []byte(expectedHashOrPlain)) == 1 {
		return true
	}
	candidateHash := HashPassword(candidate)
	return subtle.ConstantTimeCompare([]byte(candidateHash), []byte(expectedHashOrPlain)) == 1
}
