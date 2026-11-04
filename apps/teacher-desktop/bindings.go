package main

import (
	"context"
	"errors"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	"localmesh/internal/desktopconfig"
	"localmesh/internal/logging"

	"github.com/BurntSushi/toml"
	"localmesh/internal/domain/command"
	"localmesh/internal/storage/sqlite"

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

// RunScriptOnStudents queues a script execution for the given devices
// ("*" = all); results arrive as agent audit events.
func (s *ControlService) RunScriptOnStudents(_ context.Context, req remoteshell.ScriptExecutionRequest, deviceIDs []string) error {
	payload, _ := json.Marshal(req)
	if len(deviceIDs) == 0 {
		s.queueCommand("*", "script.exec", string(payload))
		return nil
	}
	for _, id := range deviceIDs {
		s.queueCommand(id, "script.exec", string(payload))
	}
	return nil
}

// SystemRestrictionView is one lockdown switch for the console UI.
type SystemRestrictionView struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

// SystemRestrictions lists the available lockdown switches.
func (s *ControlService) SystemRestrictions(_ context.Context) []SystemRestrictionView {
	return []SystemRestrictionView{
		{Name: string(restrictions.RestrictTaskMgr), Label: "禁用任务管理器"},
		{Name: string(restrictions.RestrictUSBStorage), Label: "禁用 USB 存储（需管理员）"},
		{Name: string(restrictions.RestrictWallpaper), Label: "锁定桌面壁纸"},
		{Name: string(restrictions.RestrictControlPanel), Label: "禁用控制面板"},
		{Name: string(restrictions.RestrictRegistryEdit), Label: "禁用注册表编辑器"},
	}
}

// ApplySystemPolicyToStudents pushes the lockdown switch set to students.
func (s *ControlService) ApplySystemPolicyToStudents(_ context.Context, enabled []string) error {
	payload, _ := json.Marshal(map[string]any{"enabled": enabled})
	s.queueCommand("*", "system_policy.apply", string(payload))
	return nil
}

// QueueCollect issues a homework collection instruction to all students.
func (s *ControlService) QueueCollect(_ context.Context, pattern string) error {
	payload, _ := json.Marshal(map[string]string{"pattern": pattern})
	s.queueCommand("*", "file.collect", string(payload))
	return nil
}

// ToggleRemoteControl opens or closes the remote-control gate on a student
// machine. Returns the resulting state.
func (s *ControlService) ToggleRemoteControl(ctx context.Context, deviceID string, enable bool) (bool, error) {
	if enable {
		if err := s.dialInput(ctx, deviceID); err != nil {
			return false, err
		}
		s.queueCommand(deviceID, "remote_control.start", "")
	} else {
		s.queueCommand(deviceID, "remote_control.stop", "")
		s.closeInput(deviceID)
	}
	return enable, nil
}

// IssueToGroup queues a generic control action to every device in a group.
// Supported actions: screen.lock, screen.unlock, remote_control.start/stop.
func (s *ControlService) IssueToGroup(_ context.Context, groupID, action string) error {
	switch action {
	case "screen.lock", "screen.unlock", "remote_control.start", "remote_control.stop":
	default:
		return errors.New("unsupported group action: " + action)
	}
	s.queueCommand("group:"+groupID, action, "")
	return nil
}

// ConfigService exposes the runtime configuration to the settings page.
type ConfigService struct{ c *Console }

// Get returns the current configuration.
func (s *ConfigService) Get(_ context.Context) desktopconfig.Config { return s.c.cfg }

// Update persists new settings; broadcast changes take effect on the next
// stream start.
func (s *ConfigService) Update(_ context.Context, cfg desktopconfig.Config) error {
	if err := cfg.Save(desktopconfig.Path()); err != nil {
		return err
	}
	s.c.cfg = cfg
	// 日志级别立即生效；采样配置经指令通道推送 Agent
	logging.SetLevel(cfg.Log.Level)
	s.c.hub.setAgentConfig(&AgentConfigSection{
		ThumbnailIntervalMS: cfg.Thumbnails.IntervalMS,
		ThumbnailQuality:    cfg.Thumbnails.Quality,
	})
	return nil
}

// RestoreDefaults resets the config to built-in defaults and applies them.
func (s *ConfigService) RestoreDefaults(_ context.Context) (desktopconfig.Config, error) {
	cfg := desktopconfig.Defaults()
	if err := cfg.Save(desktopconfig.Path()); err != nil {
		return cfg, err
	}
	s.c.cfg = cfg
	logging.SetLevel(cfg.Log.Level)
	s.c.hub.setAgentConfig(&AgentConfigSection{
		ThumbnailIntervalMS: cfg.Thumbnails.IntervalMS,
		ThumbnailQuality:    cfg.Thumbnails.Quality,
	})
	return cfg, nil
}

// FavoriteChannels returns the favorite channel list.
func (s *ConfigService) FavoriteChannels(_ context.Context) []int {
	return s.c.cfg.UI.FavoriteChannels
}

// ConfigPath returns the config file location for display.
func (s *ConfigService) ConfigPath(_ context.Context) string {
	path, _ := filepath.Abs(desktopconfig.Path())
	return path
}

// ExportConfig asks where to save the config and writes it. Returns "" on cancel.
func (s *ConfigService) ExportConfig(ctx context.Context) (string, error) {
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      mygo.CallerWindow(ctx),
		Title:       "导出配置",
		DefaultPath: "localmesh-config.toml",
	})
	if err != nil || path == "" {
		return "", err
	}
	raw, err := toml.Marshal(s.c.cfg)
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, raw, 0o644)
}

// ImportConfig opens a TOML file and applies it immediately.
func (s *ConfigService) ImportConfig(ctx context.Context) (bool, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: mygo.CallerWindow(ctx),
		Title:  "导入配置",
		Filters: []mygo.FileFilter{{Name: "TOML", Extensions: []string{".toml"}}},
	})
	if err != nil {
		return false, err
	}
	if len(paths) == 0 || paths[0] == "" {
		return false, nil
	}
	raw, err := os.ReadFile(paths[0])
	if err != nil {
		return false, err
	}
	cfg := desktopconfig.Defaults()
	if _, err := toml.Decode(string(raw), &cfg); err != nil {
		return false, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Save(desktopconfig.Path()); err != nil {
		return false, err
	}
	s.c.cfg = cfg
	logging.SetLevel(cfg.Log.Level)
	s.c.hub.setAgentConfig(&AgentConfigSection{
		ThumbnailIntervalMS: cfg.Thumbnails.IntervalMS,
		ThumbnailQuality:    cfg.Thumbnails.Quality,
	})
	return true, nil
}

// ClassStatus is the classroom-mode state for the console chrome.
type ClassStatus struct {
	Active    bool   `json:"active"`
	StartedAt string `json:"started_at,omitempty"`
}

// Status reports whether a class scene is currently running.
func (s *SceneService) Status(_ context.Context) (ClassStatus, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	st := s.c.classStart.Format(time.RFC3339)
	if s.c.classStart.IsZero() {
		st = ""
	}
	return ClassStatus{Active: s.c.classActive, StartedAt: st}, nil
}

// SceneService orchestrates one-click class start/stop (上课/下课).
type SceneService struct{ c *Console }

// ClassSceneOptions selects which steps the class scene includes.
type ClassSceneOptions struct {
	Lock         bool `json:"lock"`
	NetBlock     bool `json:"net_block"`
	Broadcast    bool `json:"broadcast"`
	Audio        bool `json:"audio"`
	OfflineGuard bool `json:"offline_guard"`
}

// StartClass runs the 上课 scene: optional lock, network policy, broadcast
// and audio, all pushed to students where applicable.
func (s *SceneService) StartClass(_ context.Context, opts ClassSceneOptions) error {
	if opts.Lock {
		req := screenlock.LockRequest{Title: "上课模式", Message: "上课期间已锁定，请专心听讲", ShowBanner: true}
		s.c.hub.setLock(&req)
		payload, _ := json.Marshal(req)
		(&ControlService{c: s.c}).queueCommand("*", "screen.lock", string(payload))
	}
	if opts.NetBlock {
		if err := (&ControlService{c: s.c}).ApplyFirewallToStudents(netfilter.NetworkPolicy{Mode: netfilter.ModeBlockInternet}); err != nil {
			return err
		}
	}
	if opts.Broadcast {
		bs := &BroadcastService{c: s.c}
		if _, err := bs.StartTeacher(); err != nil {
			return err
		}
	}
	if opts.Audio {
		as := &BroadcastService{c: s.c}
		if _, err := as.StartAudio(); err != nil {
			return err
		}
	}
	if opts.OfflineGuard {
		if err := (&ControlService{c: s.c}).SetOfflinePolicy(OfflinePolicy{Enabled: true, LeaseSeconds: 10}); err != nil {
			return err
		}
	}

	s.c.mu.Lock()
	s.c.classActive = true
	s.c.classStart = time.Now()
	s.c.mu.Unlock()
	return nil
}

// StopClass runs the 下课 scene: unlock, network reset, broadcast/audio off.
func (s *SceneService) StopClass(_ context.Context) error {
	s.c.hub.setLock(nil)
	s.c.stopScreenStream()
	_ = s.c.broadcast.Stop()
	as := &BroadcastService{c: s.c}
	as.StopAudio()
	if err := (&ControlService{c: s.c}).ApplyFirewallToStudents(netfilter.NetworkPolicy{Mode: netfilter.ModeAllowAll}); err != nil {
		return err
	}
	s.c.hub.setLock(nil)
	s.c.hub.setOffline(&OfflinePolicy{Enabled: false})

	s.c.mu.Lock()
	s.c.classActive = false
	s.c.classStart = time.Time{}
	s.c.mu.Unlock()
	return nil
}

// GroupCastService exposes per-group broadcast scheduling (分组看不同画面).
type GroupCastService struct{ c *Console }

// StartGroupStream starts a group's stream with the given source
// ("teacher" or a demo device id).
func (s *GroupCastService) Start(ctx context.Context, groupID, source string) error {
	return s.c.groupCast.Start(ctx, groupID, source)
}

// StopGroupStream stops one group's stream.
func (s *GroupCastService) Stop(ctx context.Context, groupID, source string) error {
	return s.c.groupCast.Stop(ctx, groupID, source)
}

// Status lists per-group stream states.
func (s *GroupCastService) Status(ctx context.Context) []GroupStreamStatus {
	return s.c.groupCast.Status(ctx)
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

// AssignSeat assigns a device to a seat (persisted), freeing its previous seat.
func (s *InteractionService) AssignSeat(_ context.Context, row, col int, deviceID, studentName string) error {
	if err := s.c.seating.AssignSeat(row, col, deviceID, studentName); err != nil {
		return err
	}
	return s.c.store.UpsertSeat(context.Background(), sqlite.SeatRecord{
		ClassroomID: defaultClassroomID, Row: row, Col: col,
		StudentName: studentName, DeviceID: deviceID, UpdatedAt: time.Now(),
	})
}

// UnassignSeat frees one seat (persisted).
func (s *InteractionService) UnassignSeat(_ context.Context, row, col int) error {
	if err := s.c.seating.UnassignSeat(row, col); err != nil {
		return err
	}
	return s.c.store.ClearSeat(context.Background(), defaultClassroomID, row, col)
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

// StartTeacher begins broadcasting the teacher screen: session state plus
// the real multicast capture stream.
func (s *BroadcastService) StartTeacher() (BroadcastStatus, error) {
	if !s.c.broadcast.IsActive() {
		if err := s.c.broadcast.StartTeacherBroadcast("teacher-desktop"); err != nil {
			return s.status(), err
		}
	} else {
		_ = s.c.broadcast.SwitchToTeacher()
	}
	if err := s.c.startScreenStream("teacher"); err != nil && err.Error() != "screen stream already running" {
		return s.status(), err
	}
	return s.status(), nil
}

// StartDemo switches the broadcast to a student's screen: the console
// relays the student's MJPEG endpoint onto the classroom multicast group.
func (s *BroadcastService) StartDemo(deviceID string) (BroadcastStatus, error) {
	if !s.c.broadcast.IsActive() {
		_ = s.c.broadcast.StartTeacherBroadcast("teacher-desktop")
	}
	if err := s.c.broadcast.SwitchToStudentDemo(deviceID); err != nil {
		return s.status(), err
	}
	if err := s.c.startScreenStream(deviceID); err != nil && err.Error() != "screen stream already running" {
		return s.status(), err
	}
	return s.status(), nil
}

// StopBroadcast ends the broadcast session and the multicast stream.
func (s *BroadcastService) Stop() (BroadcastStatus, error) {
	s.stopDemoDirectPush()
	s.c.stopScreenStream()
	_ = s.c.broadcast.Stop()
	return s.status(), nil
}

// groupMulticastPort derives a stable per-group port on the channel octet.
func groupMulticastPort(groupID string) int {
	h := 2166136261
	for _, b := range []byte(groupID) {
		h ^= int(b)
		h *= 16777619
	}
	return 8901 + (h & 0x7F) // 8901..9027，避开 8899 主流与 8900 音频基址
}

// StartGroupBroadcast targets the main screen stream at one group: the group
// members are told to join a dedicated multicast port, the console fans the
// same encoded frames out to it.
func (s *BroadcastService) StartGroupBroadcast(_ context.Context, groupID string) error {
	port := groupMulticastPort(groupID)
	if err := s.c.startScreenStream("teacher"); err != nil && err.Error() != "screen stream already running" {
		return err
	}
	if err := s.c.fanOutToGroup(groupID, port); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"addr": fmt.Sprintf("239.255.42.1:%d", port)})
	ctl := &ControlService{c: s.c}
	ctl.queueCommand("group:"+groupID, "broadcast.join_group", string(payload))
	return nil
}

// StartDemoDirectPush switches 学生演示 to direct push: the demo student
// multicasts its own screen; the teacher stops relaying and only signals.
func (s *BroadcastService) StartDemoDirectPush(_ context.Context, deviceID string) error {
	if !s.c.broadcast.IsActive() {
		_ = s.c.broadcast.StartTeacherBroadcast("teacher-desktop")
	}
	if err := s.c.broadcast.SwitchToStudentDemo(deviceID); err != nil {
		return err
	}
	// 教师停掉自己的推流（会话互斥），学生直推接管多播地址
	s.c.stopScreenStream()
	addr := s.c.currentScreenMulticast()
	payload, _ := json.Marshal(map[string]string{"addr": addr})
	ctl := &ControlService{c: s.c}
	ctl.queueCommand(deviceID, "broadcast.demo_publish", string(payload))
	return nil
}

// stopDemoDirectPush recalls the demo student's publisher.
func (s *BroadcastService) stopDemoDirectPush() {
	// 记忆最后演示的设备太薄；停播即广播级恢复（下次 StartTeacher 全量重启）
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
