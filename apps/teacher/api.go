package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"sync"
	"time"

	"localmesh/internal/capabilities/attendance"
	"localmesh/internal/capabilities/audio"
	"localmesh/internal/capabilities/broadcast"
	"localmesh/internal/capabilities/filetransfer"
	"localmesh/internal/capabilities/interaction"
	"localmesh/internal/capabilities/inventory"
	"localmesh/internal/capabilities/netfilter"
	"localmesh/internal/capabilities/power"
	"localmesh/internal/capabilities/quiz"
	"localmesh/internal/capabilities/remoteshell"
	"localmesh/internal/capabilities/restrictions"
	"localmesh/internal/capabilities/screen"
	"localmesh/internal/capabilities/screenlock"
	"localmesh/internal/capabilities/whiteboard"
	"localmesh/internal/classroom"
	"localmesh/internal/domain/discovery"
)

const defaultClassroomID = "CLASS-DEFAULT"

//go:embed all:web
var webAssets embed.FS

type APIServer struct {
	mu                sync.RWMutex
	discoveryReg      *discovery.Registry
	chatMgr           *interaction.ChatManager
	raiseHandQ        *interaction.RaiseHandQueue
	seatingChart      *interaction.SeatingChart
	thumbCache        *screen.ThumbnailCache
	powerSvc          *power.Service
	lockMgr           *screenlock.Manager
	procMon           *restrictions.ProcessMonitor
	quizSession       *quiz.QuizSession
	buzzer            *quiz.BuzzerSession
	netfilterMgr      *netfilter.Manager
	fileRcvr          *filetransfer.Receiver
	broadcastSession  *broadcast.BroadcastSession
	whiteboard        *whiteboard.Whiteboard
	shellRunner       *remoteshell.Runner
	inventoryMgr      *inventory.Manager
	attendanceSession *attendance.AttendanceSession
	audioSender       *audio.Sender
	audioCapturer     audio.Capturer
	audioCancel       context.CancelFunc
	channelID         int
}

func NewAPIServer(
	reg *discovery.Registry,
	chat *interaction.ChatManager,
	hands *interaction.RaiseHandQueue,
	seating *interaction.SeatingChart,
	thumbs *screen.ThumbnailCache,
	powerSvc *power.Service,
	lockMgr *screenlock.Manager,
	procMon *restrictions.ProcessMonitor,
	nfMgr *netfilter.Manager,
	invMgr *inventory.Manager,
) *APIServer {
	bcastSess := broadcast.NewBroadcastSession(defaultClassroomID)

	return &APIServer{
		discoveryReg:     reg,
		chatMgr:          chat,
		raiseHandQ:       hands,
		seatingChart:     seating,
		thumbCache:       thumbs,
		powerSvc:         powerSvc,
		lockMgr:          lockMgr,
		procMon:          procMon,
		buzzer:           quiz.NewBuzzerSession(),
		netfilterMgr:     nfMgr,
		fileRcvr:         filetransfer.NewReceiver(),
		broadcastSession: bcastSess,
		whiteboard:       whiteboard.NewWhiteboard(defaultClassroomID),
		shellRunner:      remoteshell.NewRunner(),
		inventoryMgr:     invMgr,
		channelID:        1,
	}
}

func (s *APIServer) Handler() http.Handler {
	mux := http.NewServeMux()

	// Core & Discovery
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/candidates", s.handleCandidates)

	// Classroom Channel & Multi-Room Isolation
	mux.HandleFunc("GET /api/v1/classroom/channel", s.handleGetChannel)
	mux.HandleFunc("POST /api/v1/classroom/channel", s.handleSetChannel)

	// Real-time Audio Streaming (WASAPI Loopback)
	mux.HandleFunc("POST /api/v1/audio/start", s.handleStartAudio)
	mux.HandleFunc("POST /api/v1/audio/stop", s.handleStopAudio)
	mux.HandleFunc("GET /api/v1/audio/status", s.handleGetAudioStatus)

	// Remote Control & Policies
	mux.HandleFunc("POST /api/v1/commands/power", s.handlePowerCommand)
	mux.HandleFunc("POST /api/v1/commands/lock", s.handleLockCommand)
	mux.HandleFunc("POST /api/v1/commands/restrictions", s.handleRestrictionsCommand)
	mux.HandleFunc("POST /api/v1/commands/firewall", s.handleFirewallCommand)
	mux.HandleFunc("GET /api/v1/commands/firewall", s.handleGetFirewallPolicy)

	// Interaction & Classroom
	mux.HandleFunc("GET /api/v1/chat/messages", s.handleGetChat)
	mux.HandleFunc("POST /api/v1/chat/messages", s.handleSendChat)
	mux.HandleFunc("GET /api/v1/raisehand", s.handleGetHands)
	mux.HandleFunc("POST /api/v1/raisehand/ack", s.handleAckHand)
	mux.HandleFunc("POST /api/v1/raisehand/clear", s.handleClearHand)
	mux.HandleFunc("GET /api/v1/seating", s.handleGetSeating)
	mux.HandleFunc("POST /api/v1/seating", s.handleSetSeating)
	mux.HandleFunc("GET /api/v1/thumbnails", s.handleGetThumbnails)
	mux.HandleFunc("POST /api/v1/thumbnails/push", s.handlePushThumbnail)

	// Quiz & Buzzer
	mux.HandleFunc("POST /api/v1/quiz/create", s.handleCreateQuiz)
	mux.HandleFunc("GET /api/v1/quiz/active", s.handleGetActiveQuiz)
	mux.HandleFunc("POST /api/v1/quiz/submit", s.handleSubmitQuiz)
	mux.HandleFunc("GET /api/v1/quiz/stats", s.handleGetQuizStats)
	mux.HandleFunc("POST /api/v1/buzzer/start", s.handleStartBuzzer)
	mux.HandleFunc("POST /api/v1/buzzer/press", s.handlePressBuzzer)
	mux.HandleFunc("GET /api/v1/buzzer/status", s.handleGetBuzzerStatus)
	mux.HandleFunc("POST /api/v1/buzzer/reset", s.handleResetBuzzer)

	// Advanced: Screen Broadcast & Student Demo
	mux.HandleFunc("POST /api/v1/broadcast/start", s.handleStartBroadcast)
	mux.HandleFunc("POST /api/v1/broadcast/stop", s.handleStopBroadcast)
	mux.HandleFunc("GET /api/v1/broadcast/status", s.handleGetBroadcastStatus)

	// Advanced: Screen Whiteboard & Collaborative Drawing
	mux.HandleFunc("GET /api/v1/whiteboard/strokes", s.handleGetWhiteboardStrokes)
	mux.HandleFunc("POST /api/v1/whiteboard/strokes", s.handleAddWhiteboardStroke)
	mux.HandleFunc("POST /api/v1/whiteboard/undo", s.handleUndoWhiteboard)
	mux.HandleFunc("POST /api/v1/whiteboard/redo", s.handleRedoWhiteboard)
	mux.HandleFunc("POST /api/v1/whiteboard/clear", s.handleClearWhiteboard)

	// Advanced: Remote Script Execution
	mux.HandleFunc("POST /api/v1/scripts/execute", s.handleExecuteScript)

	// Advanced: Asset Inventory & Telemetry
	mux.HandleFunc("GET /api/v1/inventory/telemetry", s.handleGetInventory)

	// Advanced: Attendance & Roll Call
	mux.HandleFunc("POST /api/v1/attendance/start", s.handleStartAttendance)
	mux.HandleFunc("POST /api/v1/attendance/checkin", s.handleCheckInAttendance)
	mux.HandleFunc("GET /api/v1/attendance/stats", s.handleGetAttendanceStats)
	mux.HandleFunc("GET /api/v1/attendance/export", s.handleExportAttendanceCSV)

	// Embedded static web UI
	sub, err := fs.Sub(webAssets, "web")
	if err == nil {
		mux.Handle("/", http.FileServer(http.FS(sub)))
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<h1>LocalMesh Teacher Console</h1><p>Embedded web assets not found</p>"))
		})
	}

	return mux
}

func (s *APIServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "healthy",
		"component": "localmesh-teacher",
		"version":   2,
		"time":      time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *APIServer) handleCandidates(w http.ResponseWriter, _ *http.Request) {
	if s.discoveryReg == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	candidates := s.discoveryReg.Candidates(time.Now(), 60*time.Second)
	out := make([]map[string]any, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, map[string]any{
			"device_id": string(c.DeviceID),
			"endpoint":  c.Address.String(),
			"method":    string(c.Method),
			"seen_at":   c.SeenAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *APIServer) handlePowerCommand(w http.ResponseWriter, r *http.Request) {
	var req power.PowerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.powerSvc != nil {
		_ = s.powerSvc.Execute(r.Context(), req)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": req.Action})
}

func (s *APIServer) handleLockCommand(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Action        string `json:"action"`
		Title         string `json:"title"`
		Message       string `json:"message"`
		LockInputOnly bool   `json:"lock_input_only"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if s.lockMgr != nil {
		if payload.Action == "unlock" {
			_ = s.lockMgr.Unlock()
		} else {
			_ = s.lockMgr.Lock(screenlock.LockRequest{
				Title:         payload.Title,
				Message:       payload.Message,
				LockInputOnly: payload.LockInputOnly,
				ShowBanner:    true,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": payload.Action})
}

func (s *APIServer) handleRestrictionsCommand(w http.ResponseWriter, r *http.Request) {
	var pol restrictions.ProcessPolicy
	if err := json.NewDecoder(r.Body).Decode(&pol); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.procMon != nil {
		_, _ = s.procMon.EvaluateAndEnforce(r.Context(), pol)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": pol.Mode})
}

func (s *APIServer) handleFirewallCommand(w http.ResponseWriter, r *http.Request) {
	var pol netfilter.NetworkPolicy
	if err := json.NewDecoder(r.Body).Decode(&pol); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.netfilterMgr != nil {
		if err := s.netfilterMgr.ApplyPolicy(r.Context(), pol); err != nil {
			http.Error(w, fmt.Sprintf("apply firewall policy: %v", err), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": pol.Mode})
}

func (s *APIServer) handleGetFirewallPolicy(w http.ResponseWriter, _ *http.Request) {
	if s.netfilterMgr == nil {
		writeJSON(w, http.StatusOK, map[string]any{"mode": netfilter.ModeAllowAll})
		return
	}
	pol := s.netfilterMgr.GetPolicy()
	writeJSON(w, http.StatusOK, pol)
}

func (s *APIServer) handleGetChat(w http.ResponseWriter, _ *http.Request) {
	if s.chatMgr == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, s.chatMgr.GetHistory(interaction.ChatFilter{
		ClassroomID: defaultClassroomID,
		UserID:      "teacher",
	}))
}

func (s *APIServer) handleSendChat(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Message string `json:"message"`
		Target  string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.chatMgr != nil {
		_, _ = s.chatMgr.SendMessage(interaction.ChatMessage{
			ClassroomID: defaultClassroomID,
			SenderID:    "teacher",
			SenderName:  "Teacher",
			SenderRole:  interaction.RoleTeacher,
			TargetID:    payload.Target,
			Content:     payload.Message,
			Timestamp:   time.Now().UTC(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *APIServer) handleGetHands(w http.ResponseWriter, _ *http.Request) {
	if s.raiseHandQ == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, s.raiseHandQ.ListActive(defaultClassroomID))
}

func (s *APIServer) handleAckHand(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		DeviceID string `json:"device_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	if s.raiseHandQ != nil && payload.DeviceID != "" {
		_, _ = s.raiseHandQ.Acknowledge(defaultClassroomID, payload.DeviceID, "teacher")
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *APIServer) handleClearHand(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		DeviceID string `json:"device_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	if s.raiseHandQ != nil {
		if payload.DeviceID == "" {
			_ = s.raiseHandQ.ClearAll(defaultClassroomID)
		} else {
			_ = s.raiseHandQ.Clear(defaultClassroomID, payload.DeviceID)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *APIServer) handleGetSeating(w http.ResponseWriter, _ *http.Request) {
	if s.seatingChart == nil {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	writeJSON(w, http.StatusOK, s.seatingChart.ToGrid())
}

func (s *APIServer) handleSetSeating(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Row         int    `json:"row"`
		Col         int    `json:"col"`
		DeviceID    string `json:"device_id"`
		StudentName string `json:"student_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.seatingChart != nil {
		_ = s.seatingChart.AssignSeat(
			payload.Row,
			payload.Col,
			payload.DeviceID,
			payload.StudentName,
		)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *APIServer) handleGetThumbnails(w http.ResponseWriter, _ *http.Request) {
	if s.thumbCache == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, s.thumbCache.List())
}

// maxThumbnailPushBytes bounds the per-frame upload from student agents.
const maxThumbnailPushBytes = 2 << 20 // 2 MB

// handlePushThumbnail receives a JPEG screen thumbnail pushed by a student
// agent and stores it in the thumbnail cache for console polling.
func (s *APIServer) handlePushThumbnail(w http.ResponseWriter, r *http.Request) {
	deviceID := r.Header.Get("X-Device-ID")
	if deviceID == "" {
		http.Error(w, "missing X-Device-ID header", http.StatusBadRequest)
		return
	}
	if s.thumbCache == nil {
		http.Error(w, "thumbnail cache unavailable", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxThumbnailPushBytes))
	if err != nil {
		http.Error(w, fmt.Sprintf("read frame body: %v", err), http.StatusBadRequest)
		return
	}

	seq, _ := strconv.ParseUint(r.Header.Get("X-Frame-Seq"), 10, 64)
	if _, err := s.thumbCache.UpdateRaw(deviceID, body, screen.FormatJPEG, 0, 0, seq); err != nil {
		http.Error(w, fmt.Sprintf("store thumbnail: %v", err), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Quiz Handlers
func (s *APIServer) handleCreateQuiz(w http.ResponseWriter, r *http.Request) {
	var q quiz.Quiz
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := q.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	session := quiz.NewQuizSession(q)
	if err := session.Start(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	s.quizSession = session
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"quiz_id": q.ID,
		"title":   q.Title,
		"state":   session.State(),
	})
}

func (s *APIServer) handleGetActiveQuiz(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	session := s.quizSession
	s.mu.RUnlock()

	if session == nil {
		writeJSON(w, http.StatusOK, map[string]any{"active": false})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"active":      session.State() == quiz.StateActive,
		"state":       session.State(),
		"quiz":        session.Quiz(),
		"time_remain": session.RemainingSeconds(),
	})
}

func (s *APIServer) handleSubmitQuiz(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	session := s.quizSession
	s.mu.RUnlock()

	if session == nil {
		http.Error(w, "no active quiz session", http.StatusBadRequest)
		return
	}

	var sub quiz.Submission
	if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	graded, err := session.Submit(sub)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, graded)
}

func (s *APIServer) handleGetQuizStats(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	session := s.quizSession
	s.mu.RUnlock()

	if session == nil {
		writeJSON(w, http.StatusOK, map[string]any{"active": false})
		return
	}

	stats := session.CalculateStats()
	writeJSON(w, http.StatusOK, stats)
}

// Buzzer Handlers
func (s *APIServer) handleStartBuzzer(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Question   string `json:"question"`
		MaxWinners int    `json:"max_winners"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if s.buzzer == nil {
		s.buzzer = quiz.NewBuzzerSession()
	}

	if payload.MaxWinners > 0 {
		s.buzzer.SetMaxWinners(payload.MaxWinners)
	}

	if err := s.buzzer.Start(payload.Question); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "question": payload.Question})
}

func (s *APIServer) handlePressBuzzer(w http.ResponseWriter, r *http.Request) {
	if s.buzzer == nil {
		http.Error(w, "buzzer not initialized", http.StatusBadRequest)
		return
	}

	var payload struct {
		DeviceID    string `json:"device_id"`
		StudentName string `json:"student_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	rank, deltaMs, err := s.buzzer.Buzz(payload.DeviceID, payload.StudentName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"rank":     rank,
		"delta_ms": deltaMs,
	})
}

func (s *APIServer) handleGetBuzzerStatus(w http.ResponseWriter, _ *http.Request) {
	if s.buzzer == nil {
		writeJSON(w, http.StatusOK, map[string]any{"state": "idle", "rankings": []any{}})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"state":    s.buzzer.State(),
		"question": s.buzzer.Prompt(),
		"rankings": s.buzzer.Winners(),
	})
}

func (s *APIServer) handleResetBuzzer(w http.ResponseWriter, _ *http.Request) {
	if s.buzzer != nil {
		s.buzzer.Reset()
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Broadcast & Student Demo Handlers
func (s *APIServer) handleStartBroadcast(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Mode         string `json:"mode"`          // "teacher" | "demo"
		SourceDevice string `json:"source_device"` // if demo
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.broadcastSession == nil {
		s.broadcastSession = broadcast.NewBroadcastSession(defaultClassroomID)
	}

	if payload.Mode == "demo" && payload.SourceDevice != "" {
		if !s.broadcastSession.IsActive() {
			_ = s.broadcastSession.StartTeacherBroadcast("teacher-console")
		}
		_ = s.broadcastSession.SwitchToStudentDemo(payload.SourceDevice)
	} else {
		if !s.broadcastSession.IsActive() {
			_ = s.broadcastSession.StartTeacherBroadcast("teacher-console")
		} else {
			_ = s.broadcastSession.SwitchToTeacher()
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"mode":          s.broadcastSession.Mode(),
		"active_sender": s.broadcastSession.ActiveSenderID(),
		"active":        s.broadcastSession.IsActive(),
	})
}

func (s *APIServer) handleStopBroadcast(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.broadcastSession != nil {
		_ = s.broadcastSession.Stop()
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "active": false})
}

func (s *APIServer) handleGetBroadcastStatus(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.broadcastSession == nil {
		writeJSON(w, http.StatusOK, map[string]any{"active": false, "mode": "idle"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"active":        s.broadcastSession.IsActive(),
		"mode":          s.broadcastSession.Mode(),
		"active_sender": s.broadcastSession.ActiveSenderID(),
	})
}

// Whiteboard Handlers
func (s *APIServer) handleGetWhiteboardStrokes(w http.ResponseWriter, _ *http.Request) {
	if s.whiteboard == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, s.whiteboard.ExportStrokes())
}

func (s *APIServer) handleAddWhiteboardStroke(w http.ResponseWriter, r *http.Request) {
	var stroke whiteboard.Stroke
	if err := json.NewDecoder(r.Body).Decode(&stroke); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if s.whiteboard != nil {
		if err := s.whiteboard.AddStroke(stroke); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *APIServer) handleUndoWhiteboard(w http.ResponseWriter, _ *http.Request) {
	if s.whiteboard != nil {
		_ = s.whiteboard.Undo()
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *APIServer) handleRedoWhiteboard(w http.ResponseWriter, _ *http.Request) {
	if s.whiteboard != nil {
		_ = s.whiteboard.Redo()
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *APIServer) handleClearWhiteboard(w http.ResponseWriter, _ *http.Request) {
	if s.whiteboard != nil {
		s.whiteboard.Clear()
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Remote Script Execution Handler
func (s *APIServer) handleExecuteScript(w http.ResponseWriter, r *http.Request) {
	var req remoteshell.ScriptExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if s.shellRunner == nil {
		s.shellRunner = remoteshell.NewRunner()
	}

	result, err := s.shellRunner.Execute(r.Context(), req)
	if err != nil {
		http.Error(w, fmt.Sprintf("execution failed: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// Asset Inventory Telemetry Handler
func (s *APIServer) handleGetInventory(w http.ResponseWriter, r *http.Request) {
	if s.inventoryMgr == nil {
		coll := inventory.NewDefaultCollector()
		mgr, _ := inventory.NewManager(coll)
		s.inventoryMgr = mgr
	}

	snap, err := s.inventoryMgr.GetSnapshot(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("collect inventory: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, snap)
}

// Attendance & Roll Call Handlers
func (s *APIServer) handleStartAttendance(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Title            string                       `json:"title"`
		TimeLimitSeconds int                          `json:"time_limit_seconds"`
		Roster           []attendance.ExpectedStudent `json:"roster"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if payload.Title == "" {
		payload.Title = "课堂考勤点名"
	}
	if payload.TimeLimitSeconds <= 0 {
		payload.TimeLimitSeconds = 60
	}

	sessID := fmt.Sprintf("ATT-%d", time.Now().Unix())
	sess, err := attendance.NewAttendanceSession(
		sessID,
		defaultClassroomID,
		payload.Title,
		payload.TimeLimitSeconds,
		payload.Roster,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := sess.Start(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	s.attendanceSession = sess
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"session_id": sess.ID,
		"title":      sess.Title,
		"time_limit": sess.TimeLimitSeconds,
		"state":      sess.GetState(),
	})
}

func (s *APIServer) handleCheckInAttendance(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	sess := s.attendanceSession
	s.mu.RUnlock()

	if sess == nil {
		http.Error(w, "no active attendance session", http.StatusBadRequest)
		return
	}

	var req attendance.CheckInRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err := sess.CheckIn(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Synchronize check-in seat to SeatingChart
	if s.seatingChart != nil && req.SeatRow >= 0 && req.SeatCol >= 0 {
		_ = s.seatingChart.AssignSeat(req.SeatRow, req.SeatCol, req.DeviceID, req.StudentName)
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "student_id": req.StudentID})
}

func (s *APIServer) handleGetAttendanceStats(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	sess := s.attendanceSession
	s.mu.RUnlock()

	if sess == nil {
		writeJSON(w, http.StatusOK, map[string]any{"active": false})
		return
	}

	stats := sess.GetStats()
	writeJSON(w, http.StatusOK, map[string]any{
		"active":      sess.GetState() == attendance.SessionActive,
		"state":       sess.GetState(),
		"session_id":  sess.ID,
		"title":       sess.Title,
		"stats":       stats,
		"remain_secs": sess.RemainingSeconds(),
	})
}

func (s *APIServer) handleExportAttendanceCSV(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	sess := s.attendanceSession
	s.mu.RUnlock()

	if sess == nil {
		http.Error(w, "no attendance session to export", http.StatusBadRequest)
		return
	}

	csvBytes, err := sess.ExportCSV()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=attendance_%s.csv", sess.ID))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(csvBytes)
}

func (s *APIServer) handleGetChannel(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	ch := s.channelID
	s.mu.RUnlock()

	targetMulticast := classroom.DeriveMulticastAddr(ch, broadcast.DefaultMulticastAddr)
	targetAudio := classroom.DeriveMulticastAddr(ch, audio.DefaultAudioMulticastAddr)
	fingerprint := classroom.DeriveClassroomFingerprint(defaultClassroomID, ch)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"channel_id":       ch,
		"classroom_id":     defaultClassroomID,
		"screen_multicast": targetMulticast,
		"audio_multicast":  targetAudio,
		"fingerprint":      fingerprint,
	})
}

func (s *APIServer) handleSetChannel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChannelID int `json:"channel_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := classroom.ValidateChannelID(req.ChannelID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.channelID = req.ChannelID
	s.mu.Unlock()

	targetMulticast := classroom.DeriveMulticastAddr(req.ChannelID, broadcast.DefaultMulticastAddr)
	targetAudio := classroom.DeriveMulticastAddr(req.ChannelID, audio.DefaultAudioMulticastAddr)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"channel_id":       req.ChannelID,
		"screen_multicast": targetMulticast,
		"audio_multicast":  targetAudio,
	})
}

func (s *APIServer) handleStartAudio(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.audioCancel != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        true,
			"streaming": true,
			"message":   "audio stream already running",
		})
		return
	}

	targetAddr := classroom.DeriveMulticastAddr(s.channelID, audio.DefaultAudioMulticastAddr)
	sender, err := audio.NewSender(audio.SenderConfig{
		TargetAddr: targetAddr,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"ok":    false,
			"error": fmt.Sprintf("init audio sender: %v", err),
		})
		return
	}

	capturer := audio.NewCapturer(audio.DefaultAudioFormat)
	audioCtx, cancel := context.WithCancel(context.Background())
	s.audioSender = sender
	s.audioCapturer = capturer
	s.audioCancel = cancel

	go func() {
		_ = sender.Stream(audioCtx, capturer)
	}()

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"streaming": true,
		"target":    targetAddr,
	})
}

func (s *APIServer) handleStopAudio(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.audioCancel != nil {
		s.audioCancel()
		s.audioCancel = nil
	}
	if s.audioCapturer != nil {
		_ = s.audioCapturer.Stop()
		s.audioCapturer = nil
	}
	if s.audioSender != nil {
		_ = s.audioSender.Close()
		s.audioSender = nil
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"streaming": false,
	})
}

func (s *APIServer) handleGetAudioStatus(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	streaming := s.audioCancel != nil
	target := classroom.DeriveMulticastAddr(s.channelID, audio.DefaultAudioMulticastAddr)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"streaming": streaming,
		"target":    target,
	})
}

func writeJSON(w http.ResponseWriter, code int, val any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(val)
}
