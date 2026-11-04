package main

import (
	"time"

	"localmesh/internal/capabilities/broadcast"
	"localmesh/internal/capabilities/interaction"
	"localmesh/internal/capabilities/screenlock"

	"github.com/egoist/mygo"
)

// ClassroomDelta is the incremental classroom update pushed to the page.
// The first emission carries the full snapshot (Full=true); afterwards only
// what actually changed travels the wire: devices added or removed, and
// thumbnail frames whose sequence advanced.
type ClassroomDelta struct {
	Full    bool            `json:"full"`
	Devices []DeviceInfo    `json:"devices,omitempty"` // full list, only when Full
	Thumbs  []ThumbnailInfo `json:"thumbs,omitempty"`  // full list, only when Full
	Added   []DeviceInfo    `json:"added,omitempty"`
	Removed []string        `json:"removed,omitempty"`
	Updates []ThumbnailInfo `json:"updates,omitempty"`
}

var (
	// EventClassroomDelta carries incremental classroom snapshots.
	EventClassroomDelta = mygo.NewEvent[ClassroomDelta]("classroom:delta")
	// EventChatMessage carries one classroom chat message.
	EventChatMessage = mygo.NewEvent[interaction.ChatMessage]("chat:message")
	// EventHandsChanged carries the full active raise-hand queue.
	EventHandsChanged = mygo.NewEvent[[]*interaction.HandRecord]("hands:changed")
	// EventBroadcastMode carries the broadcast session state after a change.
	EventBroadcastMode = mygo.NewEvent[BroadcastStatus]("broadcast:mode")
	// EventLockStatus carries the screen lock state after any change,
	// including unlocks made from the tray.
	EventLockStatus = mygo.NewEvent[screenlock.LockStatus]("lock:status")
	// EventQuizSubmission carries one quiz submission as it arrives.
	EventQuizSubmission = mygo.NewEvent[QuizSubmissionNotice]("quiz:submission")
)

// QuizSubmissionNotice is the realtime submission feed payload.
type QuizSubmissionNotice struct {
	QuizID     string `json:"quiz_id"`
	StudentID  string `json:"student_id"`
	DeviceID   string `json:"device_id"`
	StudentName string `json:"student_name"`
	Score      int    `json:"score"`
	MaxScore   int    `json:"max_score"`
}

// startWatchers bridges manager activity into page events. All goroutines
// stop naturally once the window closes (Emit reports an error).
func (c *Console) startWatchers() {
	go c.watchClassroom()
	go c.watchChat()
	go c.watchHands()

	// BroadcastSession fires its callback while holding its write lock, so
	// never read the session inline; relay through a goroutine after a beat.
	c.broadcast.SetOnModeChanged(func(_, _ broadcast.BroadcastMode, _ string) {
		go func() {
			time.Sleep(50 * time.Millisecond)
			_ = EventBroadcastMode.Emit(c.win, BroadcastStatus{
				Active:       c.broadcast.IsActive(),
				Mode:         c.broadcast.Mode(),
				ActiveSender: c.broadcast.ActiveSenderID(),
			})
		}()
	})
}

// watchClassroom emits a classroom delta whenever the device set changes or
// any thumbnail frame advances (2s cadence mirrors the agent push interval).
func (c *Console) watchClassroom() {
	prevDevices := map[string]DeviceInfo{}
	prevSeq := map[string]uint64{}
	first := true

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	emit := func() bool {
		devices := c.deviceList()
		thumbs := c.thumbnailList()

		delta := ClassroomDelta{}
		if first {
			delta.Full = true
			delta.Devices = devices
			delta.Thumbs = thumbs
			first = false
		} else {
			seen := make(map[string]bool, len(devices))
			for _, d := range devices {
				seen[d.DeviceID] = true
				prev, ok := prevDevices[d.DeviceID]
				if !ok || prev.Endpoint != d.Endpoint {
					delta.Added = append(delta.Added, d)
				}
			}
			for id := range prevDevices {
				if !seen[id] {
					delta.Removed = append(delta.Removed, id)
				}
			}
			for _, t := range thumbs {
				if prevSeq[t.DeviceID] != t.Sequence {
					delta.Updates = append(delta.Updates, t)
				}
			}
		}

		prevDevices = make(map[string]DeviceInfo, len(devices))
		for _, d := range devices {
			prevDevices[d.DeviceID] = d
		}
		prevSeq = make(map[string]uint64, len(thumbs))
		for _, t := range thumbs {
			prevSeq[t.DeviceID] = t.Sequence
		}

		if !delta.Full && len(delta.Added) == 0 && len(delta.Removed) == 0 && len(delta.Updates) == 0 {
			return true // nothing changed — stay silent
		}
		return EventClassroomDelta.Emit(c.win, delta) == nil
	}

	if !emit() {
		return
	}
	for range ticker.C {
		if !emit() {
			return
		}
	}
}

// watchChat relays classroom chat messages to the page.
func (c *Console) watchChat() {
	msgs, cancel := c.chatMgr.Subscribe(defaultClassroomID, "teacher")
	defer cancel()
	for msg := range msgs {
		if EventChatMessage.Emit(c.win, msg) != nil {
			return
		}
	}
}

// watchHands relays raise-hand activity as the full active queue.
func (c *Console) watchHands() {
	records, cancel := c.raiseHandQ.Subscribe(defaultClassroomID)
	defer cancel()
	for range records {
		if EventHandsChanged.Emit(c.win, c.raiseHandQ.ListActive(defaultClassroomID)) != nil {
			return
		}
	}
}

// thumbnailList snapshots the thumbnail cache as view models.
func (c *Console) thumbnailList() []ThumbnailInfo {
	items := c.thumbs.List()
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
