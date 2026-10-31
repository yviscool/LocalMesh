package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	"localmesh/internal/domain/discovery"
)

type testTrapper struct{}

func (t *testTrapper) Trap(_ bool) error { return nil }
func (t *testTrapper) Untrap() error     { return nil }
func (t *testTrapper) IsTrapping() bool  { return false }

type mockInventoryCollector struct{}

func (m *mockInventoryCollector) CollectHardware(_ context.Context) (inventory.HardwareTelemetry, error) {
	return inventory.HardwareTelemetry{
		CPU: inventory.CPUInfo{Model: "Mock CPU", Cores: 8, UsagePercent: 25.0},
	}, nil
}

func (m *mockInventoryCollector) CollectSystem(_ context.Context) (inventory.SystemTelemetry, error) {
	return inventory.SystemTelemetry{Hostname: "MockHost", CurrentUser: "Student01"}, nil
}

func (m *mockInventoryCollector) CollectSoftware(_ context.Context) ([]inventory.SoftwareItem, error) {
	return []inventory.SoftwareItem{{DisplayName: "Go", DisplayVersion: "1.23.0"}}, nil
}

func (m *mockInventoryCollector) CollectSnapshot(_ context.Context) (inventory.InventorySnapshot, error) {
	hw, _ := m.CollectHardware(context.Background())
	sys, _ := m.CollectSystem(context.Background())
	sw, _ := m.CollectSoftware(context.Background())
	return inventory.InventorySnapshot{Hardware: hw, System: sys, InstalledSoftware: sw, CapturedAt: time.Now()}, nil
}

func setupTestServer() *APIServer {
	discReg := discovery.NewRegistry()
	chatMgr := interaction.NewChatManager()
	raiseHandQ := interaction.NewRaiseHandQueue()
	seating, _ := interaction.NewSeatingChart(defaultClassroomID, "TestLab", 8, 8)
	thumbs := screen.NewThumbnailCache(320, 180, 5*time.Second)
	lockMgr := screenlock.NewManager(&testTrapper{})
	powerSvc := power.NewService(&power.OSCommandExecutor{OS: "windows"}, nil)
	procMon := restrictions.NewProcessMonitor(nil, nil)
	mockAdapter := netfilter.NewMockFirewallAdapter()
	nfMgr := netfilter.NewManager(mockAdapter)
	invMgr, _ := inventory.NewManager(&mockInventoryCollector{})

	return NewAPIServer(
		discReg,
		chatMgr,
		raiseHandQ,
		seating,
		thumbs,
		powerSvc,
		lockMgr,
		procMon,
		nfMgr,
		invMgr,
	)
}

func TestAPIServer_HealthAndDiscovery(t *testing.T) {
	srv := setupTestServer()
	h := srv.Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse json: %v", err)
	}
	if body["status"] != "healthy" {
		t.Errorf("expected healthy, got %v", body["status"])
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/candidates", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPIServer_QuizLifecycle(t *testing.T) {
	srv := setupTestServer()
	h := srv.Handler()

	// 1. Create a Quiz
	q := quiz.Quiz{
		ID:    "QUIZ-TEST-01",
		Title: "Unit Test Quiz",
		Questions: []quiz.Question{
			{
				ID:             "q1",
				Text:           "Select true statement",
				Type:           quiz.TypeSingleChoice,
				Options:        []string{"A", "B"},
				CorrectAnswers: []int{0},
				Points:         10,
			},
		},
		TimeLimitSeconds: 60,
	}

	payload, _ := json.Marshal(q)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/quiz/create", bytes.NewReader(payload))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("create quiz failed: %d, %s", rec.Code, rec.Body.String())
	}

	// 2. Submit student answer
	sub := quiz.Submission{
		StudentID:   "STU-001",
		DeviceID:    "DEV-001",
		StudentName: "Student One",
		Answers: map[string][]int{
			"q1": {0},
		},
	}
	subPayload, _ := json.Marshal(sub)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/quiz/submit", bytes.NewReader(subPayload))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("submit quiz failed: %d, %s", rec.Code, rec.Body.String())
	}

	var graded quiz.Submission
	if err := json.Unmarshal(rec.Body.Bytes(), &graded); err != nil {
		t.Fatalf("parse submission response failed: %v", err)
	}
	if graded.Score != 10 {
		t.Errorf("expected score 10, got %d", graded.Score)
	}

	// 3. Check stats
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/quiz/stats", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("get stats failed: %d", rec.Code)
	}
	var stats quiz.QuizStats
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("parse stats failed: %v", err)
	}
	if stats.TotalParticipants != 1 || stats.AverageScore != 10 {
		t.Errorf("unexpected stats: %+v", stats)
	}
}

func TestAPIServer_Buzzer(t *testing.T) {
	srv := setupTestServer()
	h := srv.Handler()

	// 1. Start Buzzer
	startPayload := []byte(`{"question":"Who was the first programmer?","max_winners":3}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/buzzer/start", bytes.NewReader(startPayload))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("start buzzer failed: %d", rec.Code)
	}

	// 2. Student 1 press
	press1 := []byte(`{"device_id":"DEV-1","student_name":"Ada Lovelace"}`)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/buzzer/press", bytes.NewReader(press1))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("press buzzer failed: %d", rec.Code)
	}
	var rankResp struct {
		Rank int `json:"rank"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &rankResp)
	if rankResp.Rank != 1 {
		t.Errorf("expected rank 1, got %d", rankResp.Rank)
	}

	// 3. Status check
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/buzzer/status", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get buzzer status failed: %d", rec.Code)
	}

	// 4. Reset
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/buzzer/reset", bytes.NewReader([]byte("{}")))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset buzzer failed: %d", rec.Code)
	}
}

func TestAPIServer_FirewallCommand(t *testing.T) {
	srv := setupTestServer()
	h := srv.Handler()

	// Apply policy
	pol := netfilter.NetworkPolicy{
		Mode: netfilter.ModeBlockInternet,
	}
	payload, _ := json.Marshal(pol)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/commands/firewall", bytes.NewReader(payload))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("apply firewall failed: %d, %s", rec.Code, rec.Body.String())
	}

	// Query policy
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/commands/firewall", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get firewall failed: %d", rec.Code)
	}
	var curPol netfilter.NetworkPolicy
	_ = json.Unmarshal(rec.Body.Bytes(), &curPol)
	if curPol.Mode != netfilter.ModeBlockInternet {
		t.Errorf("expected mode block_internet, got %s", curPol.Mode)
	}
}

func TestAPIServer_BroadcastAndWhiteboard(t *testing.T) {
	srv := setupTestServer()
	h := srv.Handler()

	// 1. Broadcast Start (Teacher Mode)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/broadcast/start", bytes.NewReader([]byte(`{"mode":"teacher"}`)))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start broadcast failed: %d", rec.Code)
	}

	// 2. Broadcast Status
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/broadcast/status", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get broadcast status failed: %d", rec.Code)
	}

	// 3. Whiteboard Add Stroke
	stroke := whiteboard.Stroke{
		Tool:  whiteboard.ToolPen,
		Color: whiteboard.RGB(255, 0, 0),
		Width: 3,
		Points: []whiteboard.Point{
			{X: 0.1, Y: 0.2},
			{X: 0.3, Y: 0.4},
		},
	}
	p, _ := json.Marshal(stroke)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/whiteboard/strokes", bytes.NewReader(p))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("add stroke failed: %d", rec.Code)
	}

	// 4. Whiteboard Get Strokes
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/whiteboard/strokes", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get strokes failed: %d", rec.Code)
	}
	var list []whiteboard.Stroke
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Errorf("expected 1 stroke, got %d", len(list))
	}

	// 5. Whiteboard Undo
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/whiteboard/undo", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("undo failed: %d", rec.Code)
	}

	// 6. Whiteboard Clear
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/whiteboard/clear", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear failed: %d", rec.Code)
	}

	// 7. Broadcast Stop
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/broadcast/stop", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stop broadcast failed: %d", rec.Code)
	}
}

func TestAPIServer_ScriptInventoryAttendance(t *testing.T) {
	srv := setupTestServer()
	h := srv.Handler()

	// 1. Remote Script Execution
	scriptReq := remoteshell.ScriptExecutionRequest{
		ScriptType:     remoteshell.ScriptTypeBatch,
		Content:        "@echo Hello from LocalMesh",
		TimeoutSeconds: 5,
	}
	p, _ := json.Marshal(scriptReq)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/scripts/execute", bytes.NewReader(p))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("execute script failed: %d", rec.Code)
	}

	// 2. Inventory Telemetry
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/inventory/telemetry", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get inventory failed: %d", rec.Code)
	}
	var snap inventory.InventorySnapshot
	_ = json.Unmarshal(rec.Body.Bytes(), &snap)
	if snap.Hardware.CPU.Model != "Mock CPU" {
		t.Errorf("expected Mock CPU, got %s", snap.Hardware.CPU.Model)
	}

	// 3. Attendance Start
	attStart := []byte(`{"title":"English Exam Roll Call","time_limit_seconds":60,"roster":[{"student_id":"S01","student_name":"Bob"}]}`)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/attendance/start", bytes.NewReader(attStart))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start attendance failed: %d", rec.Code)
	}

	// 4. Attendance Check-in
	attCheckIn := []byte(`{"student_id":"S01","device_id":"PC-01","student_name":"Bob","seat_row":1,"seat_col":1}`)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/attendance/checkin", bytes.NewReader(attCheckIn))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("checkin failed: %d", rec.Code)
	}

	// 5. Attendance Stats
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/attendance/stats", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get stats failed: %d", rec.Code)
	}

	// 6. Attendance Export CSV
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/attendance/export", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("export csv failed: %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("S01")) {
		t.Errorf("expected S01 in exported CSV")
	}
}

func TestAPIServer_AudioAndChannel(t *testing.T) {
	srv := setupTestServer()
	h := srv.Handler()

	// 1. Get default channel
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/classroom/channel", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var chBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &chBody)
	if int(chBody["channel_id"].(float64)) != 1 {
		t.Errorf("expected channel 1, got %v", chBody["channel_id"])
	}

	// 2. Set new channel 42
	setChReq := []byte(`{"channel_id":42}`)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/classroom/channel", bytes.NewReader(setChReq))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// 3. Audio Start
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/audio/start", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// 4. Audio Status
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/audio/status", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var audioBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &audioBody)
	if audioBody["streaming"] != true {
		t.Errorf("expected audio streaming true, got %v", audioBody["streaming"])
	}

	// 5. Audio Stop
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/audio/stop", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
