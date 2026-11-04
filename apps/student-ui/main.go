// LocalMesh Student UI — the student-facing desktop app.
//
// Two windows: an always-on-top floating capsule bar (raise hand, badges)
// and an on-demand fullscreen broadcast viewer. The agent notifies this app
// over the local IPC pipe; classroom interaction goes straight to the
// teacher console's student HTTP endpoints.
package main

import (
	"context"
	"flag"
	"log"
	"time"

	"localmesh/internal/platform/common"
	"localmesh/internal/transport/ipc"

	"github.com/egoist/mygo"
)

const (
	defaultTeacherURL = "http://127.0.0.1:8080"
	agentPipe         = `\\.\pipe\localmesh-agent`
	uiPipe            = `\\.\pipe\localmesh-student-ui`
)

// UI holds the window handles and identity for the student app.
type UI struct {
	teacherURL string
	deviceID   string
	student    string

	bar      *mygo.Window
	viewer   *mygo.Window
	tray     *mygo.Tray
	notifyCh chan UiNotification
}

// UiNotification is one event pushed from the agent over IPC.
type UiNotification struct {
	Type    string `json:"type"` // "broadcast" | "file" | "quiz"
	Active  bool   `json:"active,omitempty"`
	Title   string `json:"title,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

func main() {
	var (
		flagTeacher = flag.String("teacher-url", defaultTeacherURL, "Teacher console HTTP base URL")
	)
	flag.Parse()

	if !mygo.App.RequestSingleInstanceLock() {
		return
	}

	ui := &UI{
		teacherURL: *flagTeacher,
		notifyCh:   make(chan UiNotification, 16),
	}

	// Ask the agent for our identity (serveIPC default action replies with
	// device_id) — falls back to a placeholder when the agent is absent.
	if id := queryAgentDeviceID(); id != "" {
		ui.deviceID = id
		ui.student = id
	}

	mygo.Bind(&StudentService{ui: ui})

	mygo.App.WhenReady(func() {
		ui.setupTray()
		ui.openBar()
		go ui.serveAgentNotifications()
		go ui.watchBroadcast()
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// queryAgentDeviceID asks the local agent for the stable device identity.
func queryAgentDeviceID() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rt, err := common.RuntimeForCurrentOS()
	if err != nil {
		return ""
	}
	conn, err := rt.Endpoint.Dial(ctx, agentPipe)
	if err != nil {
		return ""
	}
	defer conn.Close()

	req := ipc.Request{ID: "info-1", MessageID: "msg-info", Action: "agent.info"}
	if err := ipc.Write(conn, req, ipc.DefaultMaxMessage); err != nil {
		return ""
	}
	var resp ipc.Response
	if err := ipc.Read(conn, &resp, ipc.DefaultMaxMessage); err != nil {
		return ""
	}
	if !resp.OK {
		return ""
	}
	id, _ := resp.Payload["device_id"].(string)
	return id
}
