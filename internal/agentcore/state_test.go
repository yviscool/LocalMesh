package agentcore

import "testing"

// TestDiffFirstCycle verifies the first poll arms everything that is set.
func TestDiffFirstCycle(t *testing.T) {
	desired := Snapshot{
		ChannelID: 1,
		Lock:      `{"title":"上课"}`,
		Netfilter: `{"mode":"block_internet"}`,
		Process:   `{"mode":"blocklist","processes":["game.exe"]}`,
		Offline:   `{"enabled":true,"lease_seconds":10}`,
	}
	// last 代表 Agent 当前物理状态：启动时已在默认频道 1
	plan := Diff(Snapshot{ChannelID: 1}, desired)

	if !plan.ApplyLock || plan.ClearLock {
		t.Errorf("lock plan wrong: %+v", plan)
	}
	if !plan.ApplyNetfilter || plan.ResetNetfilter {
		t.Errorf("netfilter plan wrong: %+v", plan)
	}
	if !plan.ApplyProcess || plan.ClearProcess {
		t.Errorf("process plan wrong: %+v", plan)
	}
	if !plan.RearmOffline || plan.DisarmOffline {
		t.Errorf("offline plan wrong: %+v", plan)
	}
	if plan.RetuneChannel != 0 { // 1 == last default; no retune
		t.Errorf("unexpected channel retune: %d", plan.RetuneChannel)
	}
}

// TestDiffNoChange verifies idempotent polls produce an empty plan.
func TestDiffNoChange(t *testing.T) {
	snap := Snapshot{
		ChannelID: 3,
		Lock:      `{"title":"x"}`,
		Netfilter: `{"mode":"block_internet"}`,
		Offline:   `{"enabled":true}`,
	}
	plan := Diff(snap, snap)
	if plan != (Plan{}) {
		t.Fatalf("expected empty plan, got %+v", plan)
	}
}

// TestDiffAllowAllResets verifies allow_all clears the firewall.
func TestDiffAllowAllResets(t *testing.T) {
	last := Snapshot{Netfilter: `{"mode":"block_internet"}`}
	plan := Diff(last, Snapshot{Netfilter: `{"mode":"allow_all"}`})
	if !plan.ResetNetfilter || plan.ApplyNetfilter {
		t.Fatalf("expected reset, got %+v", plan)
	}
}

// TestDiffUnlock verifies empty lock clears enforcement.
func TestDiffUnlock(t *testing.T) {
	last := Snapshot{Lock: `{"title":"x"}`}
	plan := Diff(last, Snapshot{})
	if !plan.ClearLock || plan.ApplyLock {
		t.Fatalf("expected clear lock, got %+v", plan)
	}
}

// TestDiffChannelFollow verifies channel changes produce a retune.
func TestDiffChannelFollow(t *testing.T) {
	plan := Diff(Snapshot{ChannelID: 1}, Snapshot{ChannelID: 7})
	if plan.RetuneChannel != 7 {
		t.Fatalf("expected retune to 7, got %+v", plan)
	}
}

// TestDiffControlKeepsGuard verifies the guard stays armed while any control
// section is active even after the explicit offline policy disappears.
func TestDiffControlKeepsGuard(t *testing.T) {
	last := Snapshot{Lock: `{"title":"x"}`, Offline: `{"enabled":true}`}
	plan := Diff(last, Snapshot{Lock: `{"title":"x"` + `}`})
	if plan.DisarmOffline {
		t.Fatalf("guard must stay armed while control is active: %+v", plan)
	}
}

// TestFingerprintStability verifies equal states hash identically.
func TestFingerprintStability(t *testing.T) {
	a := Snapshot{Lock: `x`, Netfilter: `y`}
	b := Snapshot{Lock: `x`, Netfilter: `y`}
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatal("fingerprints differ for equal snapshots")
	}
	if a.Fingerprint() == (Snapshot{Lock: `z`}).Fingerprint() {
		t.Fatal("different states must not collide on 64-bit fingerprint")
	}
}
