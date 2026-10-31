package main

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"localmesh/internal/platform"
	"localmesh/internal/platform/common"
	"localmesh/internal/transport/ipc"
)

func TestVerifyExitPassword(t *testing.T) {
	plain := "TestSecret123!"
	hashed := HashPassword(plain)

	if !VerifyExitPassword(plain, hashed) {
		t.Errorf("expected plain to verify against hashed")
	}
	if !VerifyExitPassword(plain, plain) {
		t.Errorf("expected plain to verify against plain")
	}
	if VerifyExitPassword("WrongSecret", hashed) {
		t.Errorf("expected wrong password to fail")
	}
	if VerifyExitPassword("", hashed) {
		t.Errorf("expected empty password to fail")
	}
	if VerifyExitPassword(plain, "") {
		t.Errorf("expected empty target to fail")
	}
}

func TestAgentExitPasswordProtection(t *testing.T) {
	rt, err := common.RuntimeForCurrentOS()
	if err != nil {
		t.Fatalf("RuntimeForCurrentOS failed: %v", err)
	}

	var pipeAddr string
	if rt.OS == platform.Windows {
		pipeAddr = fmt.Sprintf(`\\.\pipe\test-exit-agent-%d`, time.Now().UnixNano())
	} else {
		pipeAddr = filepath.Join(t.TempDir(), "agent-exit.sock")
	}

	app, err := NewAgentApp(rt, "DEVICE-TEST-EXIT", pipeAddr, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("NewAgentApp failed: %v", err)
	}

	secret := "SuperSecretPassword999!"
	app.SetExitPassword(secret)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := app.Start(ctx, false); err != nil {
		t.Fatalf("app.Start failed: %v", err)
	}
	defer app.Stop()

	// 1. Send exit command with incorrect password
	{
		dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer dialCancel()

		conn, err := rt.Endpoint.Dial(dialCtx, pipeAddr)
		if err != nil {
			t.Fatalf("Dial pipe failed: %v", err)
		}
		defer conn.Close()

		req := ipc.Request{
			ID:     "exit-wrong-1",
			Action: "agent.exit",
			Payload: map[string]any{
				"password": "wrong-password",
			},
		}
		if err := ipc.Write(conn, req, ipc.DefaultMaxMessage); err != nil {
			t.Fatalf("Write wrong exit failed: %v", err)
		}

		var resp ipc.Response
		if err := ipc.Read(conn, &resp, ipc.DefaultMaxMessage); err != nil {
			t.Fatalf("Read wrong exit response failed: %v", err)
		}

		if resp.OK {
			t.Errorf("expected unauthorized exit attempt to be rejected, but got OK")
		}
		if resp.Code != "forbidden" {
			t.Errorf("expected code forbidden, got %q", resp.Code)
		}

		// Ensure exit channel did not close
		select {
		case <-app.ExitChan():
			t.Fatalf("exit channel should not have been closed after invalid password")
		case <-time.After(50 * time.Millisecond):
			// Passed
		}
	}

	// 2. Send exit command with correct password
	{
		dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer dialCancel()

		conn, err := rt.Endpoint.Dial(dialCtx, pipeAddr)
		if err != nil {
			t.Fatalf("Dial pipe failed: %v", err)
		}
		defer conn.Close()

		req := ipc.Request{
			ID:     "exit-valid-1",
			Action: "agent.exit",
			Payload: map[string]any{
				"password": secret,
			},
		}
		if err := ipc.Write(conn, req, ipc.DefaultMaxMessage); err != nil {
			t.Fatalf("Write valid exit failed: %v", err)
		}

		var resp ipc.Response
		if err := ipc.Read(conn, &resp, ipc.DefaultMaxMessage); err != nil {
			t.Fatalf("Read valid exit response failed: %v", err)
		}

		if !resp.OK {
			t.Errorf("expected authorized exit to succeed, got error: %v", resp.Payload)
		}

		// Verify exit channel is closed
		select {
		case <-app.ExitChan():
			// Successfully triggered exit!
		case <-time.After(1 * time.Second):
			t.Errorf("expected ExitChan to be closed after valid exit command")
		}
	}
}

func TestAgentSessionAndHeartbeatIPC(t *testing.T) {
	rt, err := common.RuntimeForCurrentOS()
	if err != nil {
		t.Fatalf("RuntimeForCurrentOS failed: %v", err)
	}

	var pipeAddr string
	if rt.OS == platform.Windows {
		pipeAddr = fmt.Sprintf(`\\.\pipe\test-sess-agent-%d`, time.Now().UnixNano())
	} else {
		pipeAddr = filepath.Join(t.TempDir(), "agent-sess.sock")
	}

	app, err := NewAgentApp(rt, "DEVICE-TEST-SESS", pipeAddr, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("NewAgentApp failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := app.Start(ctx, false); err != nil {
		t.Fatalf("app.Start failed: %v", err)
	}
	defer app.Stop()

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dialCancel()

	conn, err := rt.Endpoint.Dial(dialCtx, pipeAddr)
	if err != nil {
		t.Fatalf("Dial pipe failed: %v", err)
	}
	defer conn.Close()

	// 1. Start Session
	{
		req := ipc.Request{
			ID:     "sess-start-1",
			Action: "agent.session.start",
			Payload: map[string]any{
				"session_id": "CLASS-ROOM-99",
			},
		}
		if err := ipc.Write(conn, req, ipc.DefaultMaxMessage); err != nil {
			t.Fatalf("Write session start failed: %v", err)
		}
		var resp ipc.Response
		if err := ipc.Read(conn, &resp, ipc.DefaultMaxMessage); err != nil {
			t.Fatalf("Read session start failed: %v", err)
		}
		if !resp.OK {
			t.Errorf("expected session start OK, got %v", resp)
		}
		if !app.offlineGuard.InSession() {
			t.Errorf("expected offline guard in session")
		}
	}

	// 2. Heartbeat
	{
		req := ipc.Request{
			ID:     "hb-1",
			Action: "agent.heartbeat",
		}
		if err := ipc.Write(conn, req, ipc.DefaultMaxMessage); err != nil {
			t.Fatalf("Write heartbeat failed: %v", err)
		}
		var resp ipc.Response
		if err := ipc.Read(conn, &resp, ipc.DefaultMaxMessage); err != nil {
			t.Fatalf("Read heartbeat failed: %v", err)
		}
		if !resp.OK {
			t.Errorf("expected heartbeat OK, got %v", resp)
		}
	}

	// 3. End Session
	{
		req := ipc.Request{
			ID:     "sess-end-1",
			Action: "agent.session.end",
		}
		if err := ipc.Write(conn, req, ipc.DefaultMaxMessage); err != nil {
			t.Fatalf("Write session end failed: %v", err)
		}
		var resp ipc.Response
		if err := ipc.Read(conn, &resp, ipc.DefaultMaxMessage); err != nil {
			t.Fatalf("Read session end failed: %v", err)
		}
		if !resp.OK {
			t.Errorf("expected session end OK, got %v", resp)
		}
		if app.offlineGuard.InSession() {
			t.Errorf("expected offline guard left session")
		}
	}
}
