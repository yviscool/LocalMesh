package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"localmesh/internal/capabilities/netfilter"
	"localmesh/internal/capabilities/restrictions"
	"localmesh/internal/capabilities/screenlock"
	"localmesh/internal/storage/sqlite"

	"github.com/egoist/mygo"
)

// OfflinePolicy configures what a student machine does when it loses the
// teacher: with Enabled, the agent locks the screen once the heartbeat
// lease expires (and releases when connectivity returns).
type OfflinePolicy struct {
	Enabled      bool `json:"enabled"`
	LeaseSeconds int  `json:"lease_seconds,omitempty"`
}

// AgentConfigSection is the runtime sampling configuration agents follow.
type AgentConfigSection struct {
	ThumbnailIntervalMS int `json:"thumbnail_interval_ms"`
	ThumbnailQuality    int `json:"thumbnail_quality"`
}

// AgentDesiredState is the idempotent control state every agent receives on
// each poll. Nil sections mean "no control active" for that area.
type AgentDesiredState struct {
	ChannelID  int                         `json:"channel_id"`
	Lock       *screenlock.LockRequest     `json:"lock,omitempty"`
	Netfilter  *netfilter.NetworkPolicy    `json:"netfilter,omitempty"`
	Process    *restrictions.ProcessPolicy `json:"process,omitempty"`
	Offline    *OfflinePolicy              `json:"offline,omitempty"`
	AgentCfg   *AgentConfigSection         `json:"agent_config,omitempty"`
}

// AgentCommand is a one-shot instruction queued for one device or all.
type AgentCommand struct {
	ID        string    `json:"id"`
	Action    string    `json:"action"`
	Payload   string    `json:"payload,omitempty"`
	Target    string    `json:"target"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// AgentEvent is one audit record an agent reports back.
type AgentEvent struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
	At     string `json:"at"`
}

const commandTTL = 10 * time.Minute

// agentHub holds the desired agent state, the pending command queue and the
// per-device acknowledgement bookkeeping.
type agentHub struct {
	mu       sync.Mutex
	state    AgentDesiredState
	commands []AgentCommand
	acked    map[string]map[string]bool // command id -> device -> acked
}

func newAgentHub() *agentHub {
	return &agentHub{acked: map[string]map[string]bool{}}
}

func randomID(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(b[:]))
}

// setLock updates the desired lock state for every student machine.
func (h *agentHub) setLock(req *screenlock.LockRequest) {
	h.mu.Lock()
	h.state.Lock = req
	h.mu.Unlock()
}

// setNetfilter updates the desired network policy for student machines.
func (h *agentHub) setNetfilter(pol *netfilter.NetworkPolicy) {
	h.mu.Lock()
	h.state.Netfilter = pol
	h.mu.Unlock()
}

// setProcess updates the desired process policy for student machines.
func (h *agentHub) setProcess(pol *restrictions.ProcessPolicy) {
	h.mu.Lock()
	h.state.Process = pol
	h.mu.Unlock()
}

// setOffline updates the desired offline behavior.
func (h *agentHub) setOffline(pol *OfflinePolicy) {
	h.mu.Lock()
	h.state.Offline = pol
	h.mu.Unlock()
}

// setAgentConfig updates the runtime sampling configuration.
func (h *agentHub) setAgentConfig(cfg *AgentConfigSection) {
	h.mu.Lock()
	h.state.AgentCfg = cfg
	h.mu.Unlock()
}

// setChannel updates the desired classroom channel.
func (h *agentHub) setChannel(id int) {
	h.mu.Lock()
	h.state.ChannelID = id
	h.mu.Unlock()
}

// snapshot copies the current desired state.
func (h *agentHub) snapshot() AgentDesiredState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state
}

// issue queues a one-shot command. target "*" reaches every device.
func (h *agentHub) issue(target, action, payload string) AgentCommand {
	now := time.Now()
	cmd := AgentCommand{
		ID:        randomID("cmd"),
		Action:    action,
		Payload:   payload,
		Target:    target,
		IssuedAt:  now,
		ExpiresAt: now.Add(commandTTL),
	}
	h.mu.Lock()
	h.commands = append(h.commands, cmd)
	h.mu.Unlock()
	return cmd
}

// poll returns the desired state plus commands still pending for the device,
// recording its acknowledgements and dropping expired entries. Group targets
// ("group:<id>") expand to every member device.
func (c *Console) poll(deviceID string, acked []string) (AgentDesiredState, []AgentCommand) {
	h := c.hub
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, id := range acked {
		if h.acked[id] == nil {
			h.acked[id] = map[string]bool{}
		}
		h.acked[id][deviceID] = true
	}

	pending := make([]AgentCommand, 0, len(h.commands))
	kept := h.commands[:0]
	now := time.Now()
	for _, cmd := range h.commands {
		if now.After(cmd.ExpiresAt) {
			continue // expired
		}
		reaches := cmd.Target == "*" || cmd.Target == deviceID
		if !reaches && strings.HasPrefix(cmd.Target, "group:") {
			for _, member := range c.GroupDevices(strings.TrimPrefix(cmd.Target, "group:")) {
				if member == deviceID {
					reaches = true
					break
				}
			}
		}
		alreadyAcked := h.acked[cmd.ID][deviceID]
		if reaches && !alreadyAcked {
			pending = append(pending, cmd)
		}
		kept = append(kept, cmd)
	}
	h.commands = kept

	return h.state, pending
}

// EventAgentAudit carries one agent-reported audit record to the page.
var EventAgentAudit = mygo.NewEvent[sqlite.AgentEvent]("agent:audit")

// recordEvents persists agent-reported audit records and relays them to
// the console UI.
func (c *Console) recordEvents(deviceID string, channelID int, events []AgentEvent) {
	if len(events) == 0 {
		return
	}
	records := make([]sqlite.AgentEvent, 0, len(events))
	for _, e := range events {
		occurred, err := time.Parse(time.RFC3339, e.At)
		if err != nil {
			occurred = time.Now()
		}
		records = append(records, sqlite.AgentEvent{
			EventID: e.ID, DeviceID: deviceID, ChannelID: channelID,
			Kind: e.Kind, Detail: e.Detail, OccurredAt: occurred,
		})
	}
	if err := c.store.AppendAgentEvents(context.Background(), records); err != nil {
		return
	}
	if c.win == nil {
		return // test context or window not yet created
	}
	for _, record := range records {
		_ = EventAgentAudit.Emit(c.win, record)
	}
}
