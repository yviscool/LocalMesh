package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"localmesh/internal/capabilities/netfilter"
	"localmesh/internal/storage/sqlite"
)

func testStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store
}

// TestAgentHubCommandFlow verifies: issue broadcast → device A receives it,
// acks on next poll → A no longer receives it, device B still does.
func TestAgentHubCommandFlow(t *testing.T) {
	hub := newAgentHub()
	hub.issue("*", "power.lock", "{}")

	state, cmdsA := hub.poll("DEVICE-A", nil)
	if len(cmdsA) != 1 || cmdsA[0].Action != "power.lock" {
		t.Fatalf("expected 1 pending command for A, got %d", len(cmdsA))
	}
	if state.ChannelID == 0 {
		// state always carries a channel (default 1 comes from console wiring;
		// bare hub is 0 here — acceptable, just document).
		t.Log("bare hub channel is 0; console wires default 1")
	}

	// A acks; B has not polled yet.
	_, cmdsA2 := hub.poll("DEVICE-A", []string{cmdsA[0].ID})
	if len(cmdsA2) != 0 {
		t.Fatalf("expected acked command to disappear for A, got %d", len(cmdsA2))
	}
	_, cmdsB := hub.poll("DEVICE-B", nil)
	if len(cmdsB) != 1 {
		t.Fatalf("expected broadcast still pending for B, got %d", len(cmdsB))
	}

	// Targeted command reaches only its target (B acks the broadcast first).
	_, _ = hub.poll("DEVICE-B", []string{cmdsB[0].ID})
	hub.issue("DEVICE-B", "screen.lock", "{}")
	_, cmdsA3 := hub.poll("DEVICE-A", nil)
	if len(cmdsA3) != 0 {
		t.Fatalf("targeted command leaked to A: %d", len(cmdsA3))
	}
	_, cmdsB2 := hub.poll("DEVICE-B", nil)
	if len(cmdsB2) != 1 || cmdsB2[0].Action != "screen.lock" {
		t.Fatalf("expected targeted command for B, got %+v", cmdsB2)
	}
}

// TestAgentHubStateUpdate verifies desired-state sections update idempotently.
func TestAgentHubStateUpdate(t *testing.T) {
	hub := newAgentHub()

	hub.setLock(nil)
	snap := hub.snapshot()
	if snap.Lock != nil {
		t.Fatal("expected no lock by default")
	}

	hub.setNetfilter(&netfilter.NetworkPolicy{Mode: netfilter.ModeBlockSites, BlockedDomains: []string{"game.com"}})
	hub.setProcess(nil)
	snap = hub.snapshot()
	if snap.Netfilter == nil || snap.Netfilter.Mode != netfilter.ModeBlockSites {
		t.Fatalf("expected block_sites netfilter policy, got %+v", snap.Netfilter)
	}
}

// TestHandleAgentPollRoundTrip exercises the HTTP command channel:
// events posted by the agent land in SQLite, and issued commands ride back.
func TestHandleAgentPollRoundTrip(t *testing.T) {
	store := testStore(t)
	console := &Console{store: store, hub: newAgentHub()}

	// 1. Agent polls with an audit event.
	body, _ := json.Marshal(map[string]any{
		"device_id": "DEVICE-A",
		"acked":     []string{},
		"events": []map[string]string{{
			"id":     "evt-1",
			"kind":   "process.blocked",
			"detail": "game.exe (PID 4242) terminated=true",
			"at":     "2026-11-04T01:00:00Z",
		}},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/agent/poll", bytes.NewReader(body))
	console.handleAgentPoll(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		State    AgentDesiredState `json:"state"`
		Commands []AgentCommand    `json:"commands"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode poll response: %v", err)
	}

	// 2. The event is queryable.
	events, err := store.ListAgentEvents(context.Background(), 10, "")
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 || events[0].Kind != "process.blocked" {
		t.Fatalf("expected 1 stored event, got %+v", events)
	}

	// 3. A queued command rides the next poll.
	console.hub.issue("*", "netfilter.apply", "{}")
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/agent/poll", bytes.NewReader([]byte(`{"device_id":"DEVICE-A","acked":[],"events":[]}`)))
	console.handleAgentPoll(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Commands) != 1 || resp.Commands[0].Action != "netfilter.apply" {
		t.Fatalf("expected command in poll response, got %+v", resp.Commands)
	}
}
