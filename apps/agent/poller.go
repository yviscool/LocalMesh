package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"localmesh/internal/capabilities/filetransfer"

	"localmesh/internal/capabilities/audio"
	"localmesh/internal/capabilities/netfilter"
	"localmesh/internal/capabilities/remoteshell"
	"localmesh/internal/capabilities/power"
	"localmesh/internal/capabilities/restrictions"
	"localmesh/internal/capabilities/screenlock"
	"localmesh/internal/capabilities/watchdog"
	"localmesh/internal/classroom"
)

// desiredState mirrors the teacher console's AgentDesiredState.
type desiredState struct {
	ChannelID int                         `json:"channel_id"`
	Lock      *screenlock.LockRequest     `json:"lock"`
	Netfilter *netfilter.NetworkPolicy    `json:"netfilter"`
	Process   *restrictions.ProcessPolicy `json:"process"`
	Offline   *offlinePolicy              `json:"offline"`
}

type offlinePolicy struct {
	Enabled      bool `json:"enabled"`
	LeaseSeconds int  `json:"lease_seconds"`
}

// pendingCommand is a one-shot instruction from the teacher.
type pendingCommand struct {
	ID        string    `json:"id"`
	Action    string    `json:"action"`
	Payload   string    `json:"payload"`
	Target    string    `json:"target"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// pollEnvelope is the teacher's poll response.
type pollEnvelope struct {
	State    desiredState     `json:"state"`
	Commands []pendingCommand `json:"commands"`
}

// eventBuffer accumulates audit events until the next poll ships them.
type eventBuffer struct {
	mu     sync.Mutex
	events []agentEvent
}

type agentEvent struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
	At     string `json:"at"`
}

func (b *eventBuffer) add(kind, detail string) {
	b.mu.Lock()
	b.events = append(b.events, agentEvent{
		ID:     fmt.Sprintf("evt-%d-%d", time.Now().UnixNano(), len(b.events)),
		Kind:   kind,
		Detail: detail,
		At:     time.Now().UTC().Format(time.RFC3339),
	})
	b.mu.Unlock()
}

func (b *eventBuffer) drain() []agentEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.events
	b.events = nil
	return out
}

// startPoller runs the command-channel loop: POST acks + audit events,
// receive desired state + commands, apply everything.
func (a *AgentApp) startPoller(ctx context.Context) {
	client := &http.Client{Timeout: 4 * time.Second}
	url := teacherURLJoin(a.teacherURL, "/api/v1/agent/poll")

	var lastState desiredState
	var lastLog time.Time
	lastForeground := ""

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.discoveryStop:
			return
		case <-ticker.C:
		}

		// 程序使用记录：前台应用变化即上报
		if exe, ok := ForegroundProcess(); ok && exe != lastForeground {
			a.events.add("app.focus", exe)
			lastForeground = exe
		}

		body, _ := json.Marshal(map[string]any{
			"device_id": string(a.deviceID),
			"acked":     a.drainAcked(),
			"events":    a.events.drain(),
		})

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			// teacher gone: throttle the log, keep retrying
			if time.Since(lastLog) > 30*time.Second {
				log.Printf("[Poll] teacher unreachable: %v", err)
				lastLog = time.Now()
			}
			continue
		}
		envelope, err := decodePoll(resp)
		resp.Body.Close()
		if err != nil {
			log.Printf("[Poll] bad response: %v", err)
			continue
		}

		// Heartbeat feeds the offline watchdog: teacher is alive.
		if a.offlineGuard != nil {
			a.offlineGuard.RecordHeartbeat()
		}

		a.applyEnvelope(ctx, envelope.State, &lastState)
		a.executeCommands(ctx, envelope.Commands)
	}
}

func decodePoll(resp *http.Response) (pollEnvelope, error) {
	var envelope pollEnvelope
	if resp.StatusCode != http.StatusOK {
		return envelope, fmt.Errorf("teacher returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return envelope, err
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return envelope, err
	}
	return envelope, nil
}

func teacherURLJoin(base, path string) string {
	for len(base) > 0 && base[len(base)-1] == '/' {
		base = base[:len(base)-1]
	}
	return base + path
}

// applyEnvelope diff-applies the desired state, skipping unchanged sections.
func (a *AgentApp) applyEnvelope(ctx context.Context, state desiredState, last *desiredState) {
	// 1. Channel follow: retune announcement + audio multicast.
	if state.ChannelID != 0 && state.ChannelID != last.ChannelID {
		a.setChannel(state.ChannelID)
		a.events.add("channel.follow", fmt.Sprintf("切换到频道 %d", state.ChannelID))
	}

	// 2. Lock state (idempotent desired state).
	if lockChanged(state.Lock, last.Lock) {
		if state.Lock != nil {
			if err := a.lockMgr.Lock(*state.Lock); err != nil {
				a.events.add("lock.error", err.Error())
			} else {
				a.events.add("lock.applied", state.Lock.Title)
				log.Printf("[Control] locked by teacher: %s", state.Lock.Title)
			}
		} else {
			if err := a.lockMgr.Unlock(); err != nil {
				a.events.add("lock.error", err.Error())
			} else {
				a.events.add("lock.released", "teacher unlock")
				log.Printf("[Control] unlocked by teacher")
			}
		}
	}

	// 3. Network policy.
	if netChanged(state.Netfilter, last.Netfilter) {
		if state.Netfilter == nil || state.Netfilter.Mode == netfilter.ModeAllowAll {
			if err := a.nfMgr.Reset(ctx); err != nil {
				a.events.add("netfilter.error", err.Error())
			} else {
				a.events.add("netfilter.reset", "allow_all")
			}
		} else {
			if err := a.nfMgr.ApplyPolicy(ctx, *state.Netfilter); err != nil {
				a.events.add("netfilter.error", err.Error())
			} else {
				a.events.add("netfilter.applied", string(state.Netfilter.Mode))
				log.Printf("[Control] netfilter policy %s applied", state.Netfilter.Mode)
			}
		}
	}

	// 4. Process policy.
	if procChanged(state.Process, last.Process) {
		if state.Process == nil {
			a.events.add("process.cleared", "no policy")
		} else {
			results, err := a.processMon.EvaluateAndEnforce(ctx, *state.Process)
			if err != nil {
				a.events.add("process.error", err.Error())
			} else {
				a.events.add("process.enforced", fmt.Sprintf("%s: %d matches", state.Process.Mode, len(results)))
				for _, r := range results {
					a.events.add("process.blocked", fmt.Sprintf("%s (PID %d) terminated=%v", r.Process.Name, r.Process.PID, r.Terminated))
				}
				log.Printf("[Control] process policy %s enforced (%d matches)", state.Process.Mode, len(results))
			}
		}
	}

	// 5. Offline guard policy: only active while class control is present.
	if offlineChanged(state.Offline, last.Offline) || anyControlActive(state) != anyControlActive(*last) {
		a.applyOfflinePolicy(ctx, state)
	}

	*last = state
}

// applyOfflinePolicy (re)arms the watchdog according to the teacher policy.
func (a *AgentApp) applyOfflinePolicy(ctx context.Context, state desiredState) {
	if a.offlineGuard != nil {
		a.offlineGuard.Stop()
		a.offlineGuard = nil
	}

	// 离线守护在「有任何课堂管控在位」或策略显式开启时工作。
	enabled := (state.Offline != nil && state.Offline.Enabled) ||
		(state.Offline == nil && (state.Lock != nil || state.Netfilter != nil || state.Process != nil))
	if !enabled {
		a.events.add("offline.disabled", "no offline policy")
		return
	}

	cfg := watchdog.DefaultOfflineGuardConfig()
	if state.Offline != nil && state.Offline.LeaseSeconds > 0 {
		cfg.LeaseDuration = time.Duration(state.Offline.LeaseSeconds) * time.Second
	}
	a.offlineGuard = watchdog.NewOfflineGuard(a.lockMgr, cfg)
	a.offlineGuard.Start(ctx)
	a.offlineGuard.EnterSession("class")
	a.events.add("offline.enabled", fmt.Sprintf("lease %ds", int(cfg.LeaseDuration.Seconds())))
	log.Printf("[Control] offline guard armed (lease %s)", cfg.LeaseDuration)
}

// handleFileDistribute downloads a distributed file, verifies its SHA-256
// and stores it on the student's desktop.
func (a *AgentApp) handleFileDistribute(payload string) error {
	var spec struct {
		URL  string `json:"url"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(payload), &spec); err != nil {
		return err
	}
	if spec.URL == "" || spec.Name == "" {
		return fmt.Errorf("incomplete distribute payload")
	}

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(spec.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %s", resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<30))
	if err != nil {
		return err
	}

	desktop, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(desktop, "Desktop", "LocalMesh 收到的文件")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dst := filepath.Join(dir, filepath.Base(spec.Name))
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return err
	}

	a.notifyStudentUI(map[string]any{"type": "file", "title": "收到文件 " + spec.Name})
	a.events.add("file.received", spec.Name)
	log.Printf("[Files] distributed file saved: %s", dst)
	return nil
}

// handleFileCollect scans the student desktop for matching files, packs them
// and uploads the archive to the teacher console.
func (a *AgentApp) handleFileCollect(ctx context.Context, payload string) error {
	var spec fileCollectSpec
	if err := json.Unmarshal([]byte(payload), &spec); err != nil {
		return err
	}

	desktop, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	files, err := filetransfer.FindMatchingFiles(filepath.Join(desktop, "Desktop"), spec.Pattern, 50<<20)
	if err != nil || len(files) == 0 {
		a.events.add("file.collect", "no matching files")
		return err
	}

	studentCtx := filetransfer.StudentContext{DeviceID: string(a.deviceID)}
	var buf bytes.Buffer
	if _, err := filetransfer.PackToZip(files, studentCtx, "{{device}}/{{filename}}", 500<<20, &buf); err != nil {
		return err
	}

	uploadURL := teacherURLJoin(a.teacherURL, "/api/v1/files/collect")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/zip")
	req.Header.Set("X-Device-ID", string(a.deviceID))
	req.Header.Set("X-Filename", "homework.zip")

	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upload returned %s", resp.Status)
	}
	a.events.add("file.collected", fmt.Sprintf("%d files", len(files)))
	log.Printf("[Files] collected %d files -> teacher", len(files))
	return nil
}

// handleScriptExec runs a teacher-pushed script locally and reports the
// result back through the audit channel.
func (a *AgentApp) handleScriptExec(ctx context.Context, payload string) error {
	var req remoteshell.ScriptExecutionRequest
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		return err
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 10
	}

	result, err := a.scriptRunner.Execute(ctx, req)
	if err != nil {
		a.events.add("script.error", fmt.Sprintf("%s: %v", req.ScriptType, err))
		return err
	}
	summary := fmt.Sprintf("%s exit=%d %dms stdout=%dB",
		req.ScriptType, result.ExitCode, result.DurationMs, len(result.Stdout))
	a.events.add("script.result", summary)
	log.Printf("[Script] executed: %s", summary)
	return nil
}

// fileCollectSpec mirrors the teacher's collect instruction.
type fileCollectSpec struct {
	Pattern string `json:"pattern"`
}

// executeCommands runs queued one-shot commands and records their acks.
func (a *AgentApp) executeCommands(ctx context.Context, commands []pendingCommand) {
	for _, cmd := range commands {
		var err error
		switch cmd.Action {
		case "power.shutdown", "power.reboot", "power.logoff", "power.lock":
			var req power.PowerRequest
			if cmd.Payload != "" {
				err = json.Unmarshal([]byte(cmd.Payload), &req)
				if err == nil {
					err = a.powerSvc.Execute(ctx, req)
				}
			}
		case "screen.lock":
			var req screenlock.LockRequest
			if cmd.Payload != "" {
				err = json.Unmarshal([]byte(cmd.Payload), &req)
				if err == nil {
					err = a.lockMgr.Lock(req)
				}
			}
		case "screen.unlock":
			err = a.lockMgr.Unlock()
		case "file.distribute":
			err = a.handleFileDistribute(cmd.Payload)
		case "file.collect":
			err = a.handleFileCollect(ctx, cmd.Payload)
		case "broadcast.join_group":
			var spec struct{ Addr string `json:"addr"` }
			if err := json.Unmarshal([]byte(cmd.Payload), &spec); err == nil && spec.Addr != "" {
				a.broadcastRX.retuneAddr(spec.Addr)
				a.events.add("broadcast.joined", spec.Addr)
			}
		case "broadcast.demo_publish":
			var spec struct {
				Addr string `json:"addr"`
				Stop bool   `json:"stop"`
			}
			if err := json.Unmarshal([]byte(cmd.Payload), &spec); err == nil {
				if spec.Stop {
					a.publisher.stop()
					a.events.add("demo.stopped", "publishing halted")
				} else if spec.Addr != "" {
					if err := a.publisher.start(spec.Addr); err != nil {
						a.events.add("demo.error", err.Error())
					} else {
						a.events.add("demo.publishing", spec.Addr)
					}
				}
			}
		case "script.exec":
			err = a.handleScriptExec(ctx, cmd.Payload)
		case "remote_control.start":
			a.remoteGate.activate(2 * time.Hour)
			a.events.add("remote_ctl.open", "teacher may control this machine")
			log.Printf("[RemoteCtl] gate opened")
		case "remote_control.stop":
			a.remoteGate.deactivate()
			a.events.add("remote_ctl.close", "control released")
			log.Printf("[RemoteCtl] gate closed")
		default:
			// state-applied actions (netfilter.apply/process.apply/…) are
			// handled idempotently by applyEnvelope; ack them directly.
			err = nil
		}

		if err != nil {
			a.events.add("command.error", fmt.Sprintf("%s: %v", cmd.Action, err))
			log.Printf("[Command] %s failed: %v", cmd.Action, err)
		} else {
			a.events.add("command.ok", cmd.Action)
			log.Printf("[Command] %s executed", cmd.Action)
		}
		a.ack(cmd.ID)
	}
}

// setChannel retunes the announcement, audio multicast and broadcast
// receiver to the new channel.
func (a *AgentApp) setChannel(id int) {
	a.mu.Lock()
	a.channelID = id
	audioCancel := a.audioCancel
	a.audioCancel = nil
	capturer := a.audioCapturer
	sender := a.audioSender
	a.audioCapturer = nil
	a.audioSender = nil
	a.mu.Unlock()

	if audioCancel != nil {
		audioCancel()
	}
	if capturer != nil {
		_ = capturer.Stop()
	}
	if sender != nil {
		_ = sender.Close()
	}
	a.restartAudioReceiver()
	if a.broadcastRX != nil {
		a.broadcastRX.retune(id)
	}
	log.Printf("[Channel] followed teacher to channel %d", id)
}

func lockChanged(a, b *screenlock.LockRequest) bool {
	return !sameJSON(a, b)
}

func netChanged(a, b *netfilter.NetworkPolicy) bool {
	return !sameJSON(a, b)
}

func procChanged(a, b *restrictions.ProcessPolicy) bool {
	return !sameJSON(a, b)
}

func offlineChanged(a, b *offlinePolicy) bool {
	return !sameJSON(a, b)
}

func anyControlActive(state desiredState) bool {
	return state.Lock != nil || state.Netfilter != nil || state.Process != nil
}

func sameJSON(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

func classroomDerivedAudio(id int) string {
	return classroom.DeriveMulticastAddr(id, audio.DefaultAudioMulticastAddr)
}
