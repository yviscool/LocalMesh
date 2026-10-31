package power

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestBuildMagicPacket(t *testing.T) {
	tests := []struct {
		name      string
		macHex    string
		expectErr bool
	}{
		{
			name:      "Valid standard MAC",
			macHex:    "001122334455",
			expectErr: false,
		},
		{
			name:      "Valid broadcast style MAC",
			macHex:    "AABBCCDDEEFF",
			expectErr: false,
		},
		{
			name:      "Invalid short MAC (4 bytes)",
			macHex:    "00112233",
			expectErr: true,
		},
		{
			name:      "Invalid long MAC (8 bytes EUI-64)",
			macHex:    "0011223344556677",
			expectErr: true,
		},
		{
			name:      "Invalid empty MAC",
			macHex:    "",
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			macBytes, _ := hex.DecodeString(tc.macHex)
			mac := net.HardwareAddr(macBytes)

			packet, err := BuildMagicPacket(mac)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error for mac %q, got nil", tc.macHex)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(packet) != magicPacketLen {
				t.Fatalf("expected packet length %d, got %d", magicPacketLen, len(packet))
			}

			// Verify 6 bytes of 0xFF
			for i := 0; i < 6; i++ {
				if packet[i] != 0xFF {
					t.Fatalf("expected byte %d to be 0xFF, got 0x%02X", i, packet[i])
				}
			}

			// Verify 16 repetitions of MAC
			for i := 0; i < 16; i++ {
				offset := 6 + i*macLen
				chunk := packet[offset : offset+macLen]
				if !bytes.Equal(chunk, mac) {
					t.Fatalf("chunk %d at offset %d expected %x, got %x", i, offset, mac, chunk)
				}
			}
		})
	}
}

func TestSendWakeOnLANOverUDP(t *testing.T) {
	// Start a local UDP listener to receive the magic packet
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("failed to listen UDP: %v", err)
	}
	defer listener.Close()

	targetMAC, err := net.ParseMAC("02:00:00:00:00:01")
	if err != nil {
		t.Fatalf("parse MAC: %v", err)
	}

	expectedPacket, err := BuildMagicPacket(targetMAC)
	if err != nil {
		t.Fatalf("build magic packet: %v", err)
	}

	done := make(chan struct{})
	var received []byte
	go func() {
		defer close(done)
		buf := make([]byte, 256)
		_ = listener.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, readErr := listener.ReadFrom(buf)
		if readErr == nil {
			received = buf[:n]
		}
	}()

	err = SendWakeOnLAN(targetMAC, listener.LocalAddr().String())
	if err != nil {
		t.Fatalf("SendWakeOnLAN failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for UDP packet")
	}

	if !bytes.Equal(received, expectedPacket) {
		t.Fatalf("received packet mismatch\nexpected: %x\ngot:      %x", expectedPacket, received)
	}
}

func TestPowerRequestValidation(t *testing.T) {
	tests := []struct {
		name      string
		req       PowerRequest
		expectErr error
	}{
		{
			name: "valid shutdown",
			req: PowerRequest{
				Action:             ActionShutdown,
				GracePeriodSeconds: 30,
				Force:              true,
				Message:            "System updating",
			},
			expectErr: nil,
		},
		{
			name: "valid reboot",
			req: PowerRequest{
				Action:             ActionReboot,
				GracePeriodSeconds: 0,
			},
			expectErr: nil,
		},
		{
			name: "valid logoff",
			req: PowerRequest{
				Action: ActionLogoff,
			},
			expectErr: nil,
		},
		{
			name: "valid lock",
			req: PowerRequest{
				Action: ActionLock,
			},
			expectErr: nil,
		},
		{
			name: "valid wol",
			req: PowerRequest{
				Action: ActionWOL,
				MAC:    "00:11:22:33:44:55",
			},
			expectErr: nil,
		},
		{
			name: "negative grace period",
			req: PowerRequest{
				Action:             ActionShutdown,
				GracePeriodSeconds: -5,
			},
			expectErr: ErrInvalidGracePeriod,
		},
		{
			name: "wol missing mac",
			req: PowerRequest{
				Action: ActionWOL,
			},
			expectErr: ErrMissingMAC,
		},
		{
			name: "wol invalid mac format",
			req: PowerRequest{
				Action: ActionWOL,
				MAC:    "invalid-mac-string",
			},
			expectErr: ErrInvalidMAC,
		},
		{
			name: "wol non 6-byte mac",
			req: PowerRequest{
				Action: ActionWOL,
				MAC:    "00:11:22:33:44:55:66:77",
			},
			expectErr: ErrInvalidMAC,
		},
		{
			name: "unknown action",
			req: PowerRequest{
				Action: "hibernate",
			},
			expectErr: ErrInvalidAction,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if tc.expectErr != nil {
				if !errors.Is(err, tc.expectErr) {
					t.Fatalf("expected error %v, got %v", tc.expectErr, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestParseRequest(t *testing.T) {
	t.Run("valid payload", func(t *testing.T) {
		payload := []byte(`{"action":"reboot","grace_period_seconds":10,"force":true}`)
		req, err := ParseRequest(payload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if req.Action != ActionReboot || req.GracePeriodSeconds != 10 || !req.Force {
			t.Fatalf("unexpected parsed values: %+v", req)
		}
	})

	t.Run("empty payload", func(t *testing.T) {
		_, err := ParseRequest(nil)
		if !errors.Is(err, ErrInvalidPayload) {
			t.Fatalf("expected ErrInvalidPayload, got %v", err)
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		_, err := ParseRequest([]byte(`{not-json`))
		if !errors.Is(err, ErrInvalidPayload) {
			t.Fatalf("expected ErrInvalidPayload, got %v", err)
		}
	})

	t.Run("invalid content", func(t *testing.T) {
		_, err := ParseRequest([]byte(`{"action":"invalid"}`))
		if !errors.Is(err, ErrInvalidAction) {
			t.Fatalf("expected ErrInvalidAction, got %v", err)
		}
	})
}

type mockExecutor struct {
	shutdownCalls []shutdownCall
	rebootCalls   []rebootCall
	logoffCalls   []logoffCall
	lockCalls     int
	errToReturn   error
}

type shutdownCall struct {
	force       bool
	gracePeriod time.Duration
	msg         string
}

type rebootCall struct {
	force       bool
	gracePeriod time.Duration
	msg         string
}

type logoffCall struct {
	force bool
}

func (m *mockExecutor) Shutdown(_ context.Context, force bool, gracePeriod time.Duration, msg string) error {
	m.shutdownCalls = append(m.shutdownCalls, shutdownCall{force: force, gracePeriod: gracePeriod, msg: msg})
	return m.errToReturn
}

func (m *mockExecutor) Reboot(_ context.Context, force bool, gracePeriod time.Duration, msg string) error {
	m.rebootCalls = append(m.rebootCalls, rebootCall{force: force, gracePeriod: gracePeriod, msg: msg})
	return m.errToReturn
}

func (m *mockExecutor) Logoff(_ context.Context, force bool) error {
	m.logoffCalls = append(m.logoffCalls, logoffCall{force: force})
	return m.errToReturn
}

func (m *mockExecutor) Lock(_ context.Context) error {
	m.lockCalls++
	return m.errToReturn
}

func TestServiceExecution(t *testing.T) {
	ctx := context.Background()

	t.Run("shutdown execution", func(t *testing.T) {
		mock := &mockExecutor{}
		svc := NewService(mock, nil)

		req := PowerRequest{
			Action:             ActionShutdown,
			GracePeriodSeconds: 15,
			Force:              true,
			Message:            "Closing time",
		}

		if err := svc.Execute(ctx, req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(mock.shutdownCalls) != 1 {
			t.Fatalf("expected 1 shutdown call, got %d", len(mock.shutdownCalls))
		}
		call := mock.shutdownCalls[0]
		if !call.force || call.gracePeriod != 15*time.Second || call.msg != "Closing time" {
			t.Fatalf("call mismatch: %+v", call)
		}
	})

	t.Run("reboot execution", func(t *testing.T) {
		mock := &mockExecutor{}
		svc := NewService(mock, nil)

		req := PowerRequest{
			Action:             ActionReboot,
			GracePeriodSeconds: 5,
			Force:              false,
			Message:            "Rebooting",
		}

		if err := svc.Execute(ctx, req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(mock.rebootCalls) != 1 {
			t.Fatalf("expected 1 reboot call, got %d", len(mock.rebootCalls))
		}
		call := mock.rebootCalls[0]
		if call.force || call.gracePeriod != 5*time.Second || call.msg != "Rebooting" {
			t.Fatalf("call mismatch: %+v", call)
		}
	})

	t.Run("logoff execution", func(t *testing.T) {
		mock := &mockExecutor{}
		svc := NewService(mock, nil)

		req := PowerRequest{
			Action: ActionLogoff,
			Force:  true,
		}

		if err := svc.Execute(ctx, req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(mock.logoffCalls) != 1 || !mock.logoffCalls[0].force {
			t.Fatalf("unexpected logoff call: %+v", mock.logoffCalls)
		}
	})

	t.Run("lock execution", func(t *testing.T) {
		mock := &mockExecutor{}
		svc := NewService(mock, nil)

		req := PowerRequest{Action: ActionLock}

		if err := svc.Execute(ctx, req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if mock.lockCalls != 1 {
			t.Fatalf("expected 1 lock call, got %d", mock.lockCalls)
		}
	})

	t.Run("wol execution", func(t *testing.T) {
		var wolCalled bool
		var calledMAC net.HardwareAddr
		var calledAddr string

		mockWOL := func(mac net.HardwareAddr, broadcastAddr string) error {
			wolCalled = true
			calledMAC = mac
			calledAddr = broadcastAddr
			return nil
		}

		svc := NewService(nil, mockWOL)
		req := PowerRequest{
			Action:        ActionWOL,
			MAC:           "11:22:33:44:55:66",
			BroadcastAddr: "192.168.1.255:9",
		}

		if err := svc.Execute(ctx, req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !wolCalled {
			t.Fatal("expected WOL sender to be called")
		}
		if calledMAC.String() != "11:22:33:44:55:66" {
			t.Fatalf("expected MAC 11:22:33:44:55:66, got %s", calledMAC)
		}
		if calledAddr != "192.168.1.255:9" {
			t.Fatalf("expected broadcast addr 192.168.1.255:9, got %s", calledAddr)
		}
	})

	t.Run("executor error propagation", func(t *testing.T) {
		sentinelErr := errors.New("platform failed")
		mock := &mockExecutor{errToReturn: sentinelErr}
		svc := NewService(mock, nil)

		err := svc.Execute(ctx, PowerRequest{Action: ActionShutdown})
		if !errors.Is(err, sentinelErr) {
			t.Fatalf("expected sentinelErr, got %v", err)
		}
	})
}

func TestOSCommandExecutor(t *testing.T) {
	ctx := context.Background()

	var recordedCmd string
	var recordedArgs []string

	runner := func(_ context.Context, name string, args ...string) error {
		recordedCmd = name
		recordedArgs = args
		return nil
	}

	t.Run("windows shutdown command", func(t *testing.T) {
		exec := &OSCommandExecutor{Runner: runner, OS: "windows"}
		err := exec.Shutdown(ctx, true, 10*time.Second, "Class ended")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if recordedCmd != "shutdown.exe" {
			t.Fatalf("expected shutdown.exe, got %s", recordedCmd)
		}
		expectedArgs := []string{"/s", "/t", "10", "/f", "/c", "Class ended"}
		if !reflect.DeepEqual(recordedArgs, expectedArgs) {
			t.Fatalf("expected args %v, got %v", expectedArgs, recordedArgs)
		}
	})

	t.Run("windows reboot command", func(t *testing.T) {
		exec := &OSCommandExecutor{Runner: runner, OS: "windows"}
		err := exec.Reboot(ctx, false, 0, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expectedArgs := []string{"/r", "/t", "0"}
		if !reflect.DeepEqual(recordedArgs, expectedArgs) {
			t.Fatalf("expected args %v, got %v", expectedArgs, recordedArgs)
		}
	})

	t.Run("windows lock command", func(t *testing.T) {
		exec := &OSCommandExecutor{Runner: runner, OS: "windows"}
		err := exec.Lock(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if recordedCmd != "rundll32.exe" || len(recordedArgs) != 1 || recordedArgs[0] != "user32.dll,LockWorkStation" {
			t.Fatalf("unexpected lock command: %s %v", recordedCmd, recordedArgs)
		}
	})

	t.Run("linux shutdown command", func(t *testing.T) {
		exec := &OSCommandExecutor{Runner: runner, OS: "linux"}
		err := exec.Shutdown(ctx, false, 0, "Shutting down")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expectedArgs := []string{"-h", "now", "Shutting down"}
		if !reflect.DeepEqual(recordedArgs, expectedArgs) {
			t.Fatalf("expected args %v, got %v", expectedArgs, recordedArgs)
		}
	})

	t.Run("unsupported OS error", func(t *testing.T) {
		exec := &OSCommandExecutor{Runner: runner, OS: "plan9"}
		err := exec.Shutdown(ctx, false, 0, "")
		if !errors.Is(err, ErrUnsupportedAction) {
			t.Fatalf("expected ErrUnsupportedAction, got %v", err)
		}
	})
}
