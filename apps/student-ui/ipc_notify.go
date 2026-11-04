package main

import (
	"context"
	"log"
	"time"

	"localmesh/internal/platform/common"
	"localmesh/internal/transport/ipc"

	"github.com/egoist/mygo"
)

// Events pushed into the student pages.
var (
	// EventBroadcast tells pages the classroom broadcast started/stopped.
	EventBroadcast = mygo.NewEvent[UiNotification]("ui:broadcast")
	// EventNotify is a generic toast (files, quiz, chat badges).
	EventNotify = mygo.NewEvent[UiNotification]("ui:notify")
)

// serveAgentNotifications listens on the student UI pipe; the agent dials it
// to push lifecycle notifications (broadcast start/stop, files, quiz).
func (ui *UI) serveAgentNotifications() {
	rt, err := common.RuntimeForCurrentOS()
	if err != nil {
		log.Printf("[IPC] platform runtime: %v", err)
		return
	}
	listener, err := rt.Endpoint.Listen(context.Background(), uiPipe)
	if err != nil {
		log.Printf("[IPC] listen %s failed: %v", uiPipe, err)
		return
	}

	for {
		conn, err := listener.Accept(context.Background())
		if err != nil {
			return
		}
		func() {
			defer conn.Close()
			var req ipc.Request
			if err := ipc.Read(conn, &req, ipc.DefaultMaxMessage); err != nil {
				return
			}
			if req.Action != "ui.notify" {
				return
			}
			n := UiNotification{}
			if raw, ok := req.Payload["type"].(string); ok {
				n.Type = raw
			}
			if raw, ok := req.Payload["active"].(bool); ok {
				n.Active = raw
			}
			if raw, ok := req.Payload["title"].(string); ok {
				n.Title = raw
			}
			if raw, ok := req.Payload["detail"].(string); ok {
				n.Detail = raw
			}

			select {
			case ui.notifyCh <- n:
			default:
			}
			// 页面事件即时转发
			if n.Type == "broadcast" {
				_ = EventBroadcast.Emit(ui.bar, n)
			} else {
				ui.notifyPage(EventNotify, n)
			}
		}()
	}
}

// watchBroadcast reacts to agent notifications: open/close the fullscreen
// viewer as the broadcast toggles.
func (ui *UI) watchBroadcast() {
	for n := range ui.notifyCh {
		switch n.Type {
		case "broadcast":
			if n.Active {
				ui.openViewer()
			} else {
				ui.closeViewer()
			}
		}
	}
}

// raiseHand posts a raise-hand record to the teacher console.
func (ui *UI) raiseHand(reason string) error {
	body := map[string]any{
		"device_id":    ui.deviceID,
		"student_name": ui.student,
		"reason":       reason,
	}
	_, err := postTeacherJSON(ui.teacherURL+"/api/v1/raisehand", body, 3*time.Second)
	return err
}
