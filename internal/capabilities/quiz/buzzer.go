package quiz

import (
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrBuzzerNotActive    = errors.New("buzzer round is not active")
	ErrAlreadyBuzzed      = errors.New("student has already buzzed in this round")
	ErrBuzzerLimitReached = errors.New("maximum number of buzzer winners reached")
	ErrInvalidDeviceIDReq = errors.New("device id is required")
)

// BuzzerState represents the stage of the buzzer race.
type BuzzerState string

const (
	BuzzerIdle   BuzzerState = "idle"
	BuzzerActive BuzzerState = "active"
	BuzzerClosed BuzzerState = "closed"
)

// BuzzerEntry represents a student who successfully buzzed in.
type BuzzerEntry struct {
	Rank        int       `json:"rank"`
	DeviceID    string    `json:"device_id"`
	StudentName string    `json:"student_name"`
	BuzzedAt    time.Time `json:"buzzed_at"`
	DeltaMs     int64     `json:"delta_ms"` // Milliseconds from round start
}

// BuzzerSession manages a real-time 抢答 (buzzer) competition.
type BuzzerSession struct {
	mu         sync.RWMutex
	state      BuzzerState
	prompt     string
	startedAt  time.Time
	closedAt   time.Time
	maxWinners int
	entries    []BuzzerEntry
	buzzedSet  map[string]bool // deviceID -> true
}

// NewBuzzerSession initializes a new BuzzerSession in BuzzerIdle state.
func NewBuzzerSession(prompt ...string) *BuzzerSession {
	p := ""
	if len(prompt) > 0 {
		p = prompt[0]
	}
	return &BuzzerSession{
		state:     BuzzerIdle,
		prompt:    p,
		buzzedSet: make(map[string]bool),
	}
}

// SetMaxWinners sets an optional limit on winners.
func (b *BuzzerSession) SetMaxWinners(limit int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.maxWinners = limit
}

// Start activates the buzzer countdown, recording the exact start time and prompt.
func (b *BuzzerSession) Start(prompt ...string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(prompt) > 0 && prompt[0] != "" {
		b.prompt = prompt[0]
	}

	b.state = BuzzerActive
	b.startedAt = time.Now()
	b.closedAt = time.Time{}
	b.entries = nil
	b.buzzedSet = make(map[string]bool)
	return nil
}

// Close explicitly stops the buzzer session.
func (b *BuzzerSession) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state != BuzzerActive {
		return ErrBuzzerNotActive
	}

	b.state = BuzzerClosed
	b.closedAt = time.Now()
	return nil
}

// Reset resets the buzzer session to BuzzerIdle.
func (b *BuzzerSession) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.state = BuzzerIdle
	b.startedAt = time.Time{}
	b.closedAt = time.Time{}
	b.entries = nil
	b.buzzedSet = make(map[string]bool)
}

// Buzz records a student's buzz with thread-safe atomic rank ordering and millisecond precision.
// Returns (rank, deltaMs, err).
func (b *BuzzerSession) Buzz(deviceID, studentName string) (rank int, deltaMs int64, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state != BuzzerActive {
		return 0, 0, ErrBuzzerNotActive
	}

	trimmedDeviceID := strings.TrimSpace(deviceID)
	if trimmedDeviceID == "" {
		return 0, 0, ErrInvalidDeviceIDReq
	}

	if b.buzzedSet[trimmedDeviceID] {
		return 0, 0, ErrAlreadyBuzzed
	}

	if b.maxWinners > 0 && len(b.entries) >= b.maxWinners {
		return 0, 0, ErrBuzzerLimitReached
	}

	now := time.Now()
	delta := now.Sub(b.startedAt).Milliseconds()
	if delta < 0 {
		delta = 0
	}

	rank = len(b.entries) + 1
	entry := BuzzerEntry{
		Rank:        rank,
		DeviceID:    trimmedDeviceID,
		StudentName: studentName,
		BuzzedAt:    now,
		DeltaMs:     delta,
	}

	b.entries = append(b.entries, entry)
	b.buzzedSet[trimmedDeviceID] = true

	if b.maxWinners > 0 && len(b.entries) >= b.maxWinners {
		b.state = BuzzerClosed
		b.closedAt = now
	}

	return rank, delta, nil
}

// BuzzEntry is a convenience method that returns a BuzzerEntry pointer.
func (b *BuzzerSession) BuzzEntry(deviceID, studentName string) (*BuzzerEntry, error) {
	rank, delta, err := b.Buzz(deviceID, studentName)
	if err != nil {
		return nil, err
	}
	return &BuzzerEntry{
		Rank:        rank,
		DeviceID:    deviceID,
		StudentName: studentName,
		BuzzedAt:    time.Now(),
		DeltaMs:     delta,
	}, nil
}

// Winners returns the strictly ordered list of winners.
func (b *BuzzerSession) Winners() []BuzzerEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := make([]BuzzerEntry, len(b.entries))
	copy(result, b.entries)
	return result
}

// Rankings is an alias for Winners to support legacy/convenience callers.
func (b *BuzzerSession) Rankings() []BuzzerEntry {
	return b.Winners()
}

// Prompt returns the prompt for this buzzer session.
func (b *BuzzerSession) Prompt() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.prompt
}

// State returns the current buzzer state.
func (b *BuzzerSession) State() BuzzerState {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}
