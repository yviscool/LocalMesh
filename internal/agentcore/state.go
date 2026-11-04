// Package agentcore holds the pure decision logic of the student agent's
// command channel: turning "last applied state" + "desired state" into a
// concrete action plan, independent of transports and windowing.
package agentcore

import (
	"crypto/sha256"
	"encoding/hex"
)

// Snapshot is a JSON-serializable, comparable view of the desired agent
// state. Empty section strings mean "no control active" for that area.
type Snapshot struct {
	ChannelID int    `json:"channel_id"`
	Lock      string `json:"lock,omitempty"`
	Netfilter string `json:"netfilter,omitempty"`
	Process   string `json:"process,omitempty"`
	Offline   string `json:"offline,omitempty"`
}

// Fingerprint returns a stable hash of the snapshot for quick equality.
func (s Snapshot) Fingerprint() string {
	sum := sha256.Sum256([]byte(s.Lock + "|" + s.Netfilter + "|" + s.Process + "|" + s.Offline))
	return hex.EncodeToString(sum[:8])
}

// Plan enumerates the actions required to move from the last applied state
// to the desired one. Booleans map 1:1 to poller side effects.
type Plan struct {
	RetuneChannel  int  // 0 = unchanged
	ApplyLock      bool // Lock section changed to non-empty
	ClearLock      bool // Lock section changed to empty
	ApplyNetfilter bool // non-empty non-allow_all policy arrived
	ResetNetfilter bool // policy became empty or allow_all
	ApplyProcess   bool
	ClearProcess   bool
	RearmOffline   bool // offline policy changed to enabled (or control became active)
	DisarmOffline  bool // offline policy changed to disabled (and no control)
}

// allowAllMode reports whether the serialized netfilter section is the
// neutral allow_all policy (JSON-encoded by the teacher binding).
func allowAllMode(section string) bool {
	return section == `{"mode":"allow_all"}`
}

// Diff computes the action plan between the last applied snapshot and the
// desired one. Pure function — fully unit-testable.
func Diff(last, desired Snapshot) Plan {
	var plan Plan

	if desired.ChannelID != 0 && desired.ChannelID != last.ChannelID {
		plan.RetuneChannel = desired.ChannelID
	}

	if desired.Lock != last.Lock {
		if desired.Lock == "" {
			plan.ClearLock = true
		} else {
			plan.ApplyLock = true
		}
	}

	if desired.Netfilter != last.Netfilter {
		if desired.Netfilter == "" || allowAllMode(desired.Netfilter) {
			plan.ResetNetfilter = true
		} else {
			plan.ApplyNetfilter = true
		}
	}

	if desired.Process != last.Process {
		if desired.Process == "" {
			plan.ClearProcess = true
		} else {
			plan.ApplyProcess = true
		}
	}

	offlineChanged := desired.Offline != last.Offline
	desiredOfflineOn := desired.Offline != ""
	lastOfflineOn := last.Offline != ""
	if offlineChanged && desiredOfflineOn {
		plan.RearmOffline = true
	}
	if offlineChanged && !desiredOfflineOn && !hasControl(desired) {
		plan.DisarmOffline = true
	}
	// 有管控在位但策略未显式配置时，守护保持武装（上一个周期已武装）
	if !lastOfflineOn && desiredOfflineOn {
		plan.RearmOffline = true
	}
	return plan
}

// hasControl reports whether any enforcement section is active.
func hasControl(s Snapshot) bool {
	return s.Lock != "" || (s.Netfilter != "" && !allowAllMode(s.Netfilter)) || s.Process != ""
}
