//go:build windows

package windows

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

func TestEnsureSingleInstance_Table(t *testing.T) {
	tests := []struct {
		name      string
		mutexName string
		wantErr   bool
		errCheck  func(error) bool
	}{
		{
			name:      "empty mutex name",
			mutexName: "",
			wantErr:   true,
			errCheck: func(err error) bool {
				return err != nil && err != ErrAlreadyRunning
			},
		},
		{
			name:      "whitespace mutex name",
			mutexName: "   ",
			wantErr:   true,
			errCheck: func(err error) bool {
				return err != nil && err != ErrAlreadyRunning
			},
		},
		{
			name:      "valid unique mutex name",
			mutexName: fmt.Sprintf("LocalMeshTestMutex-%d", time.Now().UnixNano()),
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release, err := EnsureSingleInstance(tt.mutexName)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errCheck != nil && !tt.errCheck(err) {
					t.Fatalf("unexpected error type: %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if release == nil {
				t.Fatalf("expected release func, got nil")
			}
			defer release()

			// Trying to acquire the same mutex while held must return ErrAlreadyRunning
			_, err2 := EnsureSingleInstance(tt.mutexName)
			if !errors.Is(err2, ErrAlreadyRunning) {
				t.Fatalf("expected ErrAlreadyRunning, got %v", err2)
			}
		})
	}
}

func TestEnsureSingleInstance_ReleaseAndReacquire(t *testing.T) {
	mutexName := fmt.Sprintf("LocalMeshReacquire-%d", time.Now().UnixNano())

	release1, err := EnsureSingleInstance(mutexName)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// Double release must be safe
	release1()
	release1()

	// Should now be able to acquire again
	release2, err := EnsureSingleInstance(mutexName)
	if err != nil {
		t.Fatalf("re-acquire failed: %v", err)
	}
	defer release2()
}

func TestEnsureSingleInstance_Concurrent(t *testing.T) {
	mutexName := fmt.Sprintf("LocalMeshConcurrent-%d", time.Now().UnixNano())
	const goroutines = 10

	var (
		wg       sync.WaitGroup
		acquired int32
		rejected int32
		winnerMu sync.Mutex
		winnerFn func()
	)

	startGate := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startGate

			release, err := EnsureSingleInstance(mutexName)
			if err == nil {
				atomic.AddInt32(&acquired, 1)
				winnerMu.Lock()
				winnerFn = release
				winnerMu.Unlock()
			} else if errors.Is(err, ErrAlreadyRunning) {
				atomic.AddInt32(&rejected, 1)
			} else {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}

	close(startGate)
	wg.Wait()

	if acquired != 1 {
		t.Fatalf("expected exactly 1 winner, got %d", acquired)
	}
	if rejected != goroutines-1 {
		t.Fatalf("expected %d rejected, got %d", goroutines-1, rejected)
	}

	// Release winner
	winnerMu.Lock()
	if winnerFn != nil {
		winnerFn()
	}
	winnerMu.Unlock()

	// Subsequent acquire should now succeed
	releaseAfter, err := EnsureSingleInstance(mutexName)
	if err != nil {
		t.Fatalf("acquire after winner release failed: %v", err)
	}
	releaseAfter()
}

func TestAutostart_Lifecycle(t *testing.T) {
	appName := fmt.Sprintf("LocalMeshLifecycleTest-%d", time.Now().UnixNano())
	execPath := `C:\Program Files\LocalMesh\localmesh-agent.exe`

	t.Cleanup(func() {
		_ = SetAutostart(appName, "", false)
	})

	// Table test invalid arguments
	badArgs := []struct {
		name     string
		app      string
		path     string
		enabled  bool
		errCheck func(error) bool
	}{
		{
			name:    "empty app on enable",
			app:     "",
			path:    execPath,
			enabled: true,
			errCheck: func(err error) bool {
				return err != nil
			},
		},
		{
			name:    "empty exec on enable",
			app:     appName,
			path:    "",
			enabled: true,
			errCheck: func(err error) bool {
				return err != nil
			},
		},
		{
			name:    "empty app on disable",
			app:     "",
			path:    "",
			enabled: false,
			errCheck: func(err error) bool {
				return err != nil
			},
		},
	}

	for _, tt := range badArgs {
		t.Run(tt.name, func(t *testing.T) {
			err := SetAutostart(tt.app, tt.path, tt.enabled)
			if !tt.errCheck(err) {
				t.Fatalf("expected error, got %v", err)
			}
		})
	}

	// Initially not enabled
	enabled, err := IsAutostartEnabled(appName)
	if err != nil {
		t.Fatalf("IsAutostartEnabled failed: %v", err)
	}
	if enabled {
		t.Fatalf("expected app to not be enabled initially")
	}

	// Enable autostart
	if err := SetAutostart(appName, execPath, true); err != nil {
		t.Fatalf("SetAutostart(true) failed: %v", err)
	}

	enabled, err = IsAutostartEnabled(appName)
	if err != nil {
		t.Fatalf("IsAutostartEnabled failed: %v", err)
	}
	if !enabled {
		t.Fatalf("expected app to be enabled")
	}

	// Idempotent enable
	if err := SetAutostart(appName, execPath, true); err != nil {
		t.Fatalf("second SetAutostart(true) failed: %v", err)
	}

	// Disable autostart
	if err := SetAutostart(appName, "", false); err != nil {
		t.Fatalf("SetAutostart(false) failed: %v", err)
	}

	enabled, err = IsAutostartEnabled(appName)
	if err != nil {
		t.Fatalf("IsAutostartEnabled failed: %v", err)
	}
	if enabled {
		t.Fatalf("expected app to be disabled")
	}

	// Idempotent disable
	if err := SetAutostart(appName, "", false); err != nil {
		t.Fatalf("second SetAutostart(false) failed: %v", err)
	}
}

func TestAutostart_StartupApprovedIntegration(t *testing.T) {
	appName := fmt.Sprintf("LocalMeshApprovedTest-%d", time.Now().UnixNano())
	execPath := `C:\Program Files\LocalMesh\localmesh-agent.exe`

	t.Cleanup(func() {
		_ = SetAutostart(appName, "", false)
		if ak, err := registry.OpenKey(registry.CURRENT_USER, approvedKeyPath, registry.SET_VALUE); err == nil {
			defer ak.Close()
			_ = ak.DeleteValue(appName)
		}
	})

	// Enable autostart
	if err := SetAutostart(appName, execPath, true); err != nil {
		t.Fatalf("SetAutostart failed: %v", err)
	}

	// Verify enabled
	enabled, err := IsAutostartEnabled(appName)
	if err != nil || !enabled {
		t.Fatalf("expected enabled, got %v (err %v)", enabled, err)
	}

	// Simulate Task Manager toggling the app OFF:
	// Writes binary value with odd first byte (e.g. 0x03)
	ak, _, err := registry.CreateKey(registry.CURRENT_USER, approvedKeyPath, registry.SET_VALUE)
	if err != nil {
		t.Fatalf("failed to create StartupApproved key: %v", err)
	}
	disabledBytes := []byte{0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	if err := ak.SetBinaryValue(appName, disabledBytes); err != nil {
		ak.Close()
		t.Fatalf("failed to set disabled marker: %v", err)
	}
	ak.Close()

	// IsAutostartEnabled must now report FALSE because Task Manager disabled it
	enabled, err = IsAutostartEnabled(appName)
	if err != nil {
		t.Fatalf("IsAutostartEnabled failed: %v", err)
	}
	if enabled {
		t.Fatalf("expected IsAutostartEnabled to report false when StartupApproved marker is disabled")
	}

	// Re-enabling via SetAutostart must clear the StartupApproved marker
	if err := SetAutostart(appName, execPath, true); err != nil {
		t.Fatalf("SetAutostart re-enable failed: %v", err)
	}

	enabled, err = IsAutostartEnabled(appName)
	if err != nil {
		t.Fatalf("IsAutostartEnabled after re-enable failed: %v", err)
	}
	if !enabled {
		t.Fatalf("expected IsAutostartEnabled to report true after clearing disabled marker")
	}
}
