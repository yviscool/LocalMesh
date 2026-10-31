//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	ServiceName         = "LocalMeshAgent"
	ServiceDisplayName  = "LocalMesh Student Agent"
	ServiceDescription  = "LocalMesh background student agent and security watchdog service"
	DefaultExitPassword = "LocalMeshAdmin123!"
)

// IsWindowsService reports whether the process is launched by the Windows Service Control Manager.
func IsWindowsService() bool {
	isSvc, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	return isSvc
}

type agentServiceHandler struct {
	startApp func(ctx context.Context) error
	stopApp  func()
}

func (h *agentServiceHandler) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown

	changes <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		if err := h.startApp(ctx); err != nil {
			errChan <- err
		}
	}()

	select {
	case err := <-errChan:
		log.Printf("Service start error: %v", err)
		changes <- svc.Status{State: svc.Stopped}
		return false, 1
	case <-time.After(500 * time.Millisecond):
		// Started successfully
	}

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
	log.Printf("Service %s is now running.", ServiceName)

loop:
	for req := range r {
		switch req.Cmd {
		case svc.Interrogate:
			changes <- req.CurrentStatus
		case svc.Stop, svc.Shutdown:
			log.Printf("Service stop requested (%v)", req.Cmd)
			changes <- svc.Status{State: svc.StopPending}
			cancel()
			if h.stopApp != nil {
				h.stopApp()
			}
			break loop
		default:
			log.Printf("Unexpected service control request #%d", req.Cmd)
		}
	}

	changes <- svc.Status{State: svc.Stopped}
	return false, 0
}

// RunService runs the agent as a Windows service under SCM.
func RunService(startApp func(ctx context.Context) error, stopApp func()) error {
	handler := &agentServiceHandler{
		startApp: startApp,
		stopApp:  stopApp,
	}
	return svc.Run(ServiceName, handler)
}

// InstallService registers LocalMeshAgent with Windows SCM as a LocalSystem automatic service.
func InstallService(execPath string, args []string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %s already exists", ServiceName)
	}

	cfg := mgr.Config{
		ServiceType:      windows.SERVICE_WIN32_OWN_PROCESS,
		StartType:        mgr.StartAutomatic,
		ErrorControl:     mgr.ErrorNormal,
		DisplayName:      ServiceDisplayName,
		Description:      ServiceDescription,
		ServiceStartName: "", // empty runs as LocalSystem
	}

	s, err = m.CreateService(ServiceName, execPath, cfg, args...)
	if err != nil {
		return fmt.Errorf("create service %s: %w", ServiceName, err)
	}
	defer s.Close()

	log.Printf("Service %s successfully installed (LocalSystem, Automatic).", ServiceName)
	return nil
}

// UninstallService unregisters LocalMeshAgent from Windows SCM.
func UninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("service %s is not installed: %w", ServiceName, err)
	}
	defer s.Close()

	// Ensure service is stopped before deletion
	status, err := s.Control(svc.Stop)
	if err == nil {
		_ = status
		time.Sleep(1 * time.Second)
	}

	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service %s: %w", ServiceName, err)
	}

	log.Printf("Service %s successfully uninstalled.", ServiceName)
	return nil
}

// HashPassword calculates the SHA-256 hex digest of a password.
func HashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

// VerifyExitPassword performs constant-time comparison of a password against the expected hash or plaintext.
func VerifyExitPassword(candidate, expectedHashOrPlain string) bool {
	if candidate == "" || expectedHashOrPlain == "" {
		return false
	}
	// Check against plaintext directly
	if subtle.ConstantTimeCompare([]byte(candidate), []byte(expectedHashOrPlain)) == 1 {
		return true
	}
	// Check against SHA-256 hash
	candidateHash := HashPassword(candidate)
	return subtle.ConstantTimeCompare([]byte(candidateHash), []byte(expectedHashOrPlain)) == 1
}
