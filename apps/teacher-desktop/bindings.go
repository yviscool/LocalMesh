package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"localmesh/internal/capabilities/attendance"
	"localmesh/internal/capabilities/audio"
	"localmesh/internal/capabilities/broadcast"
	"localmesh/internal/capabilities/interaction"
	"localmesh/internal/capabilities/inventory"
	"localmesh/internal/capabilities/netfilter"
	"localmesh/internal/capabilities/power"
	"localmesh/internal/capabilities/quiz"
	"localmesh/internal/capabilities/remoteshell"
	"localmesh/internal/capabilities/restrictions"
	"localmesh/internal/capabilities/screenlock"
	"localmesh/internal/capabilities/whiteboard"
	"localmesh/internal/classroom"
	"localmesh/internal/domain/command"

	"github.com/egoist/mygo"
)

// DeviceInfo is one discovered student machine.
type DeviceInfo struct {
	DeviceID  string `json:"device_id"`
	Endpoint  string `json:"endpoint"`
	Method    string `json:"method"`
	SeenAt    string `json:"seen_at"`
	ChannelID int    `json:"channel_id"`
}

// ThumbnailInfo is one live screen preview frame (JPEG, base64-encoded by
// the JSON bridge).
type ThumbnailInfo struct {
	DeviceID  string    `json:"device_id"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Data      []byte    `json:"data"`
	Timestamp time.Time `json:"timestamp"`
	Sequence  uint64    `json:"sequence"`
}

// ChannelInfo describes the active classroom channel and its derived
// multicast addresses.
type ChannelInfo struct {
	ChannelID       int    `json:"channel_id"`
	ClassroomID     string `json:"classroom_id"`
	ScreenMulticast string `json:"screen_multicast"`
	AudioMulticast  string `json:"audio_multicast"`
	Fingerprint     string `json:"fingerprint"`
}

func channelInfo(channelID int) ChannelInfo {
	return ChannelInfo{
		ChannelID:       channelID,
		ClassroomID:     defaultClassroomID,
		ScreenMulticast: classroom.DeriveMulticastAddr(channelID, broadcast.DefaultMulticastAddr),
		AudioMulticast:  classroom.DeriveMulticastAddr(channelID, audio.DefaultAudioMulticastAddr),
		Fingerprint:     classroom.DeriveClassroomFingerprint(defaultClassroomID, channelID),
	}
}

// ClassroomService exposes device discovery, live thumbnails and channel
// selection to the frontend.
type ClassroomService struct{ c *Console }

// Devices lists every student machine seen within the discovery window.
func (s *ClassroomService) Devices() []DeviceInfo { return s.c.deviceList() }

// Thumbnails lists all live screen previews.
func (s *ClassroomService) Thumbnails() []ThumbnailInfo {
	items := s.c.thumbs.List()
	out := make([]ThumbnailInfo, 0, len(items))
	for _, t := range items {
		out = append(out, ThumbnailInfo{
			DeviceID:  t.DeviceID,
			Width:     t.Width,
			Height:    t.Height,
			Data:      t.Data,
			Timestamp: t.Timestamp,
			Sequence:  t.Sequence,
		})
	}
	return out
}

// Channel returns the active classroom channel details.
func (s *ClassroomService) Channel() ChannelInfo {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	return channelInfo(s.c.channelID)
}

// SetChannel switches the classroom channel (1-255). Students on the
// command channel follow automatically.
func (s *ClassroomService) SetChannel(_ context.Context, id int) (ChannelInfo, error) {
	if err := classroom.ValidateChannelID(id); err != nil {
		return ChannelInfo{}, err
	}
	s.c.mu.Lock()
	s.c.channelID = id
	s.c.mu.Unlock()
	s.c.hub.setChannel(id)
	return channelInfo(id), nil
}

// ControlService exposes classroom control commands.
type ControlService struct{ c *Console }

// LockScreen blanks student screens with a banner (full lock or input-only).
func (s *ControlService) LockScreen(title, message string, inputOnly bool) error {
	err := s.c.lockMgr.Lock(screenlock.LockRequest{
		Title:         title,
		Message:       message,
		LockInputOnly: inputOnly,
		ShowBanner:    true,
	})
	if err == nil {
		_ = EventLockStatus.Emit(s.c.win, s.c.lockMgr.Status())
	}
	return err
}

// UnlockScreen releases the current lock.
func (s *ControlService) UnlockScreen() error {
	err := s.c.lockMgr.Unlock()
	if err == nil {
		_ = EventLockStatus.Emit(s.c.win, s.c.lockMgr.Status())
	}
	return err
}

// LockState reports the current screen lock state.
func (s *ControlService) LockState() screenlock.LockStatus { return s.c.lockMgr.Status() }

// Power executes a power action request (shutdown/reboot/logoff/lock/wol).
func (s *ControlService) Power(ctx context.Context, req power.PowerRequest) error {
	return s.c.powerSvc.Execute(ctx, req)
}

// PolicyEnforcement is the JSON view of one process enforcement result.
type PolicyEnforcement struct {
	PID        int    `json:"pid"`
	Name       string `json:"name"`
	Restricted bool   `json:"restricted"`
	Terminated bool   `json:"terminated"`
	Error      string `json:"error,omitempty"`
}

// ApplyProcessPolicy enforces a process blocklist/allowlist.
func (s *ControlService) ApplyProcessPolicy(ctx context.Context, pol restrictions.ProcessPolicy) ([]PolicyEnforcement, error) {
	results, err := s.c.procMon.EvaluateAndEnforce(ctx, pol)
	if err != nil {
		return nil, err
	}
	out := make([]PolicyEnforcement, 0, len(results))
	for _, r := range results {
		errText := ""
		if r.Error != nil {
			errText = r.Error.Error()
		}
		out = append(out, PolicyEnforcement{
			PID:        r.Process.PID,
			Name:       r.Process.Name,
			Restricted: r.Restricted,
			Terminated: r.Terminated,
			Error:      errText,
		})
	}
	return out, nil
}

// queueCommand queues a one-shot agent command and records it in SQLite.
func (s *ControlService) queueCommand(target, action, payload string) {
	cmd := s.c.hub.issue(target, action, payload)
	env, err := command.New(
		cmd.ID, "agent.control", action,
		command.Target{Kind: command.TargetDevice, ID: target},
		cmd.ExpiresAt,
		command.RetryPolicy{MaxAttempts: 1, Backoff: 0},
	)
	if err == nil {
		_ = s.c.store.Create(context.Background(), env)
	}
}

// ApplyFirewallToStudents pushes the network policy to every student
// machine via the command channel.
func (s *ControlService) ApplyFirewallToStudents(pol netfilter.NetworkPolicy) error {
	if err := pol.Validate(); err != nil {
		return err
	}
	s.c.hub.setNetfilter(&pol)
	payload, _ := json.Marshal(pol)
	s.queueCommand("*", "netfilter.apply", string(payload))
	return nil
}

// ApplyProcessPolicyToStudents pushes the process policy to every student
// machine via the command channel.
func (s *ControlService) ApplyProcessPolicyToStudents(pol restrictions.ProcessPolicy) error {
	if err := pol.Validate(); err != nil {
		return err
	}
	s.c.hub.setProcess(&pol)
	payload, _ := json.Marshal(pol)
	s.queueCommand("*", "process.apply", string(payload))
	return nil
}

// LockStudents pushes a lock request to every student machine.
func (s *ControlService) LockStudents(title, message string, inputOnly bool) error {
	req := screenlock.LockRequest{Title: title, Message: message, LockInputOnly: inputOnly, ShowBanner: true}
	if err := req.Validate(); err != nil {
		return err
	}
	s.c.hub.setLock(&req)
	payload, _ := json.Marshal(req)
	s.queueCommand("*", "screen.lock", string(payload))
	return nil
}

// UnlockStudents releases the lock on every student machine.
func (s *ControlService) UnlockStudents() error {
	s.c.hub.setLock(nil)
	s.queueCommand("*", "screen.unlock", "")
	return nil
}

// SetOfflinePolicy configures the student offline behavior.
func (s *ControlService) SetOfflinePolicy(pol OfflinePolicy) error {
	if pol.LeaseSeconds < 0 {
		return fmt.Errorf("lease seconds must be >= 0")
	}
	s.c.hub.setOffline(&pol)
	payload, _ := json.Marshal(pol)
	s.queueCommand("*", "offline.apply", string(payload))
	return nil
}

// PowerToStudents queues a power command for the given devices ("*" = all).
func (s *ControlService) PowerToStudents(_ context.Context, action string, deviceIDs []string) error {
	req := power.PowerRequest{
		Action:             power.PowerAction(action),
		GracePeriodSeconds: 10,
	}
	if err := req.Validate(); err != nil {
		return err
	}
	payload, _ := json.Marshal(req)
	if len(deviceIDs) == 0 {
		s.queueCommand("*", "power."+action, string(payload))
		return nil
	}
	for _, id := range deviceIDs {
		s.queueCommand(id, "power."+action, string(payload))
	}
	return nil
}

// PolicyAuditView is one agent audit record for the console UI.
type PolicyAuditView struct {
	EventID    string `json:"event_id"`
	DeviceID   string `json:"device_id"`
	Kind       string `json:"kind"`
	Detail     string `json:"detail"`
	OccurredAt string `json:"occurred_at"`
}

// AuditService exposes agent-reported audit records to the console.
type AuditService struct{ c *Console }

// Recent returns the newest agent audit records, optionally per device.
func (s *AuditService) Recent(_ context.Context, limit int, deviceID string) ([]PolicyAuditView, error) {
	events, err := s.c.store.ListAgentEvents(context.Background(), limit, deviceID)
	if err != nil {
		return nil, err
	}
	out := make([]PolicyAuditView, 0, len(events))
	for _, e := range events {
		out = append(out, PolicyAuditView{
			EventID:    e.EventID,
			DeviceID:   e.DeviceID,
			Kind:       e.Kind,
			Detail:     e.Detail,
			OccurredAt: e.OccurredAt.Format(time.RFC3339),
		})
	}
	return out, nil
}

// FirewallPolicy returns the active network filtering policy.
func (s *ControlService) FirewallPolicy() netfilter.NetworkPolicy { return s.c.nfMgr.GetPolicy() }

// ApplyFirewall applies a network filtering policy.
func (s *ControlService) ApplyFirewall(ctx context.Context, pol netfilter.NetworkPolicy) error {
	return s.c.nfMgr.ApplyPolicy(ctx, pol)
}

// InteractionService exposes chat, raise-hand and seating.
type InteractionService struct{ c *Console }

// SendChat sends a classroom message; an empty target broadcasts to all.
func (s *InteractionService) SendChat(message, target string) (interaction.ChatMessage, error) {
	return s.c.chatMgr.SendMessage(interaction.ChatMessage{
		ClassroomID: defaultClassroomID,
		SenderID:    "teacher",
		SenderName:  "Teacher",
		SenderRole:  interaction.RoleTeacher,
		TargetID:    target,
		Content:     message,
		Timestamp:   time.Now().UTC(),
	})
}

// ChatHistory returns recent classroom messages.
func (s *InteractionService) ChatHistory() []interaction.ChatMessage {
	return s.c.chatMgr.GetHistory(interaction.ChatFilter{ClassroomID: defaultClassroomID})
}

// Hands lists the active raise-hand queue, FIFO order.
func (s *InteractionService) Hands() []*interaction.HandRecord {
	return s.c.raiseHandQ.ListActive(defaultClassroomID)
}

// AckHand acknowledges one raised hand.
func (s *InteractionService) AckHand(deviceID string) error {
	_, err := s.c.raiseHandQ.Acknowledge(defaultClassroomID, deviceID, "teacher")
	return err
}

// ClearHand dismisses one raised hand, or every hand when deviceID is empty.
func (s *InteractionService) ClearHand(deviceID string) error {
	if deviceID == "" {
		s.c.raiseHandQ.ClearAll(defaultClassroomID)
		return nil
	}
	return s.c.raiseHandQ.Clear(defaultClassroomID, deviceID)
}

// SeatingGrid returns the full rows x cols seating matrix.
func (s *InteractionService) SeatingGrid() [][]*interaction.Seat { return s.c.seating.ToGrid() }

// AssignSeat assigns a device to a seat, freeing its previous seat.
func (s *InteractionService) AssignSeat(_ context.Context, row, col int, deviceID, studentName string) error {
	return s.c.seating.AssignSeat(row, col, deviceID, studentName)
}

// UnassignSeat frees one seat.
func (s *InteractionService) UnassignSeat(_ context.Context, row, col int) error {
	return s.c.seating.UnassignSeat(row, col)
}

// QuizStatus describes the current quiz session for the frontend.
type QuizStatus struct {
	Active      bool              `json:"active"`
	State       quiz.SessionState `json:"state"`
	Quiz        *quiz.Quiz        `json:"quiz,omitempty"`
	TimeRemain  int               `json:"time_remain"`
	Submissions int               `json:"submissions"`
}

// AttendanceStatus describes the current attendance session.
type AttendanceStatus struct {
	Active     bool                       `json:"active"`
	State      attendance.SessionState    `json:"state"`
	SessionID  string                     `json:"session_id"`
	Title      string                     `json:"title"`
	Stats      attendance.AttendanceStats `json:"stats"`
	RemainSecs int                        `json:"remain_secs"`
}

// BuzzerStatus describes the buzzer competition state.
type BuzzerStatus struct {
	State    quiz.BuzzerState   `json:"state"`
	Question string             `json:"question"`
	Rankings []quiz.BuzzerEntry `json:"rankings"`
}

// TeachingService exposes quiz, buzzer, attendance and whiteboard.
type TeachingService struct{ c *Console }

// StartQuiz creates and launches a quiz session.
func (s *TeachingService) StartQuiz(_ context.Context, q quiz.Quiz) (QuizStatus, error) {
	if err := q.Validate(); err != nil {
		return QuizStatus{}, err
	}
	session := quiz.NewQuizSession(q)
	if err := session.Start(); err != nil {
		return QuizStatus{}, err
	}

	s.c.mu.Lock()
	s.c.quizSession = session
	s.c.mu.Unlock()

	return s.quizStatus(session), nil
}

// ActiveQuiz reports the current quiz session state.
func (s *TeachingService) ActiveQuiz() QuizStatus {
	s.c.mu.Lock()
	session := s.c.quizSession
	s.c.mu.Unlock()
	if session == nil {
		return QuizStatus{Active: false}
	}
	return s.quizStatus(session)
}

func (s *TeachingService) quizStatus(session *quiz.QuizSession) QuizStatus {
	state := session.State()
	return QuizStatus{
		Active:      state == quiz.StateActive,
		State:       state,
		Quiz:        func() *quiz.Quiz { q := session.Quiz(); return &q }(),
		TimeRemain:  session.RemainingSeconds(),
		Submissions: len(session.GetSubmissions()),
	}
}

// EndQuiz closes the running quiz session early.
func (s *TeachingService) EndQuiz() error {
	s.c.mu.Lock()
	session := s.c.quizSession
	s.c.mu.Unlock()
	if session == nil {
		return fmt.Errorf("no active quiz session")
	}
	return session.End()
}

// QuizStats returns analytics for the current quiz session.
func (s *TeachingService) QuizStats() (quiz.QuizStats, error) {
	s.c.mu.Lock()
	session := s.c.quizSession
	s.c.mu.Unlock()
	if session == nil {
		return quiz.QuizStats{}, fmt.Errorf("no active quiz session")
	}
	return session.CalculateStats(), nil
}

// StartBuzzer opens a buzzer competition.
func (s *TeachingService) StartBuzzer(question string, maxWinners int) error {
	if maxWinners > 0 {
		s.c.buzzer.SetMaxWinners(maxWinners)
	}
	return s.c.buzzer.Start(question)
}

// BuzzerStatus reports the buzzer state and current rankings.
func (s *TeachingService) BuzzerStatus() BuzzerStatus {
	return BuzzerStatus{
		State:    s.c.buzzer.State(),
		Question: s.c.buzzer.Prompt(),
		Rankings: s.c.buzzer.Winners(),
	}
}

// ResetBuzzer resets the buzzer for the next round.
func (s *TeachingService) ResetBuzzer() error {
	s.c.buzzer.Reset()
	return nil
}

// StartAttendance launches a roll-call session.
func (s *TeachingService) StartAttendance(title string, timeLimitSeconds int, roster []attendance.ExpectedStudent) (AttendanceStatus, error) {
	if title == "" {
		title = "课堂考勤点名"
	}
	if timeLimitSeconds <= 0 {
		timeLimitSeconds = 60
	}

	sess, err := attendance.NewAttendanceSession(
		fmt.Sprintf("ATT-%d", time.Now().Unix()),
		defaultClassroomID,
		title,
		timeLimitSeconds,
		roster,
	)
	if err != nil {
		return AttendanceStatus{}, err
	}
	if err := sess.Start(); err != nil {
		return AttendanceStatus{}, err
	}

	s.c.mu.Lock()
	s.c.attendance = sess
	s.c.mu.Unlock()

	return s.attendanceStatus(sess), nil
}

// AttendanceStatus reports the current attendance session.
func (s *TeachingService) AttendanceStatus() AttendanceStatus {
	s.c.mu.Lock()
	sess := s.c.attendance
	s.c.mu.Unlock()
	if sess == nil {
		return AttendanceStatus{Active: false}
	}
	return s.attendanceStatus(sess)
}

func (s *TeachingService) attendanceStatus(sess *attendance.AttendanceSession) AttendanceStatus {
	return AttendanceStatus{
		Active:     sess.GetState() == attendance.SessionActive,
		State:      sess.GetState(),
		SessionID:  sess.ID,
		Title:      sess.Title,
		Stats:      sess.GetStats(),
		RemainSecs: sess.RemainingSeconds(),
	}
}

// ExportAttendanceCSV asks where to save the report and writes it.
func (s *TeachingService) ExportAttendanceCSV(ctx context.Context) (string, error) {
	s.c.mu.Lock()
	sess := s.c.attendance
	s.c.mu.Unlock()
	if sess == nil {
		return "", fmt.Errorf("no attendance session to export")
	}

	csvBytes, err := sess.ExportCSV()
	if err != nil {
		return "", err
	}

	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      mygo.CallerWindow(ctx),
		Title:       "导出考勤报表",
		DefaultPath: fmt.Sprintf("attendance_%s.csv", sess.ID),
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil // user cancelled
	}
	if err := os.WriteFile(path, csvBytes, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// WhiteboardStrokes returns all whiteboard strokes.
func (s *TeachingService) WhiteboardStrokes() []whiteboard.Stroke {
	return s.c.whiteboard.ExportStrokes()
}

// AddWhiteboardStroke appends one stroke or command.
func (s *TeachingService) AddWhiteboardStroke(_ context.Context, stroke whiteboard.Stroke) error {
	return s.c.whiteboard.AddStroke(stroke)
}

// UndoWhiteboard reverts the last stroke.
func (s *TeachingService) UndoWhiteboard() error { return s.c.whiteboard.Undo() }

// RedoWhiteboard reapplies the last undone stroke.
func (s *TeachingService) RedoWhiteboard() error { return s.c.whiteboard.Redo() }

// ClearWhiteboard removes every stroke.
func (s *TeachingService) ClearWhiteboard() error {
	s.c.whiteboard.Clear()
	return nil
}

// BroadcastService exposes screen broadcast modes and audio relay.
type BroadcastService struct{ c *Console }

// BroadcastStatus describes the screen broadcast session.
type BroadcastStatus struct {
	Active       bool                    `json:"active"`
	Mode         broadcast.BroadcastMode `json:"mode"`
	ActiveSender string                  `json:"active_sender"`
}

func (s *BroadcastService) status() BroadcastStatus {
	return BroadcastStatus{
		Active:       s.c.broadcast.IsActive(),
		Mode:         s.c.broadcast.Mode(),
		ActiveSender: s.c.broadcast.ActiveSenderID(),
	}
}

// StartTeacher begins broadcasting the teacher screen.
func (s *BroadcastService) StartTeacher() (BroadcastStatus, error) {
	if !s.c.broadcast.IsActive() {
		if err := s.c.broadcast.StartTeacherBroadcast("teacher-desktop"); err != nil {
			return s.status(), err
		}
	} else {
		_ = s.c.broadcast.SwitchToTeacher()
	}
	return s.status(), nil
}

// StartDemo switches the broadcast to a student's screen.
func (s *BroadcastService) StartDemo(deviceID string) (BroadcastStatus, error) {
	if !s.c.broadcast.IsActive() {
		_ = s.c.broadcast.StartTeacherBroadcast("teacher-desktop")
	}
	if err := s.c.broadcast.SwitchToStudentDemo(deviceID); err != nil {
		return s.status(), err
	}
	return s.status(), nil
}

// StopBroadcast ends the broadcast session.
func (s *BroadcastService) Stop() (BroadcastStatus, error) {
	_ = s.c.broadcast.Stop()
	return s.status(), nil
}

// Status reports the broadcast session state.
func (s *BroadcastService) Status() BroadcastStatus { return s.status() }

// AudioStatus describes the classroom audio relay.
type AudioStatus struct {
	Streaming bool   `json:"streaming"`
	Target    string `json:"target"`
}

func (s *BroadcastService) audioStatus() AudioStatus {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	return AudioStatus{
		Streaming: s.c.audioCancel != nil,
		Target:    classroom.DeriveMulticastAddr(s.c.channelID, audio.DefaultAudioMulticastAddr),
	}
}

// StartAudio starts the classroom audio relay from the teacher microphone.
func (s *BroadcastService) StartAudio() (AudioStatus, error) {
	s.c.mu.Lock()
	if s.c.audioCancel != nil {
		s.c.mu.Unlock()
		return s.audioStatus(), nil
	}

	target := classroom.DeriveMulticastAddr(s.c.channelID, audio.DefaultAudioMulticastAddr)
	sender, err := audio.NewSender(audio.SenderConfig{TargetAddr: target})
	if err != nil {
		s.c.mu.Unlock()
		return AudioStatus{}, fmt.Errorf("init audio sender: %w", err)
	}

	capturer := audio.NewCapturer(audio.DefaultAudioFormat)
	audioCtx, cancel := context.WithCancel(context.Background())
	s.c.audioSender = sender
	s.c.audioCapturer = capturer
	s.c.audioCancel = cancel
	s.c.mu.Unlock()

	go func() { _ = sender.Stream(audioCtx, capturer) }()

	return AudioStatus{Streaming: true, Target: target}, nil
}

// StopAudio stops the audio relay.
func (s *BroadcastService) StopAudio() AudioStatus {
	s.c.mu.Lock()
	if s.c.audioCancel != nil {
		s.c.audioCancel()
		s.c.audioCancel = nil
	}
	if s.c.audioCapturer != nil {
		_ = s.c.audioCapturer.Stop()
		s.c.audioCapturer = nil
	}
	if s.c.audioSender != nil {
		_ = s.c.audioSender.Close()
		s.c.audioSender = nil
	}
	s.c.mu.Unlock()
	return s.audioStatus()
}

// AudioStatus reports the relay state.
func (s *BroadcastService) AudioStatus() AudioStatus { return s.audioStatus() }

// ToolsService exposes remote script execution and asset inventory.
type ToolsService struct{ c *Console }

// ExecuteScript runs a script on the teacher machine and returns its output.
func (s *ToolsService) ExecuteScript(ctx context.Context, req remoteshell.ScriptExecutionRequest) (*remoteshell.ScriptExecutionResult, error) {
	return s.c.shell.Execute(ctx, req)
}

// Inventory collects a full asset telemetry snapshot.
func (s *ToolsService) Inventory(ctx context.Context) (inventory.InventorySnapshot, error) {
	return s.c.invMgr.GetSnapshot(ctx)
}
