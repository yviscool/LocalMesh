package main

import (
	"context"
	"log"
	"time"

	"localmesh/internal/platform/common"
	"localmesh/internal/transport/ipc"
)

const uiPipeAddress = `\\.\pipe\localmesh-student-ui`

// contextWithTimeout returns a short-lived context for local IPC dials.
func contextWithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*time.Second)
}

// notifyStudentUI pushes a lifecycle notification to the local student UI
// (best-effort: the UI may not be running).
func (a *AgentApp) notifyStudentUI(notification map[string]any) {
	rt, err := common.RuntimeForCurrentOS()
	if err != nil {
		return
	}
	ctx, cancel := contextWithTimeout()
	defer cancel()
	conn, err := rt.Endpoint.Dial(ctx, uiPipeAddress)
	if err != nil {
		return // UI not running — fine
	}
	defer conn.Close()

	req := ipc.Request{
		ID:        "notify-1",
		MessageID: "msg-notify",
		Action:    "ui.notify",
		Payload:   notification,
	}
	if err := ipc.Write(conn, req, ipc.DefaultMaxMessage); err != nil {
		log.Printf("[UI] notify write failed: %v", err)
	}
}
