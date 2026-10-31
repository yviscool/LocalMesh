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

func TestAgentAppLifecycle(t *testing.T) {
	rt, err := common.RuntimeForCurrentOS()
	if err != nil {
		t.Fatalf("RuntimeForCurrentOS failed: %v", err)
	}

	var pipeAddr string
	if rt.OS == platform.Windows {
		pipeAddr = fmt.Sprintf(`\\.\pipe\test-localmesh-agent-%d`, time.Now().UnixNano())
	} else {
		pipeAddr = filepath.Join(t.TempDir(), "agent.sock")
	}

	app, err := NewAgentApp(rt, "DEVICE-TEST-001", pipeAddr, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("NewAgentApp failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := app.Start(ctx, false); err != nil {
		t.Fatalf("app.Start failed: %v", err)
	}
	defer app.Stop()

	// Verify IPC connectivity
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dialCancel()

	conn, err := rt.Endpoint.Dial(dialCtx, pipeAddr)
	if err != nil {
		t.Fatalf("Endpoint.Dial to agent pipe failed: %v", err)
	}
	defer conn.Close()

	req := ipc.Request{
		ID:        "req-test-123",
		MessageID: "msg-test-123",
		Action:    "ping",
	}
	if err := ipc.Write(conn, req, ipc.DefaultMaxMessage); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	var resp ipc.Response
	if err := ipc.Read(conn, &resp, ipc.DefaultMaxMessage); err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if resp.ID != req.ID {
		t.Errorf("expected response ID %s, got %s", req.ID, resp.ID)
	}
	if !resp.OK {
		t.Errorf("expected response OK, got false (code: %s)", resp.Code)
	}
}
