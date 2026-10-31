package interaction

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrEmptyContent       = errors.New("chat message content cannot be empty")
	ErrContentTooLong     = errors.New("chat message exceeds maximum length")
	ErrClassroomIDMissing = errors.New("classroom id is required")
	ErrSenderIDMissing    = errors.New("sender id is required")
)

const (
	MaxContentLength = 4096
	MaxChatHistory   = 1000
)

// SenderRole denotes the role of the message sender.
type SenderRole string

const (
	RoleTeacher SenderRole = "teacher"
	RoleStudent SenderRole = "student"
	RoleSystem  SenderRole = "system"
)

// ChatMessage represents a broadcast or whisper message in a classroom.
type ChatMessage struct {
	ID          string     `json:"id"`
	ClassroomID string     `json:"classroom_id"`
	SenderID    string     `json:"sender_id"`
	SenderName  string     `json:"sender_name"`
	SenderRole  SenderRole `json:"sender_role"`
	TargetID    string     `json:"target_id,omitempty"` // empty for broadcast; deviceID/userID for whisper
	Content     string     `json:"content"`
	Timestamp   time.Time  `json:"timestamp"`
	IsWhisper   bool       `json:"is_whisper"`
}

// Validate validates chat message fields.
func (m *ChatMessage) Validate() error {
	if strings.TrimSpace(m.ClassroomID) == "" {
		return ErrClassroomIDMissing
	}
	if strings.TrimSpace(m.SenderID) == "" {
		return ErrSenderIDMissing
	}
	trimmed := strings.TrimSpace(m.Content)
	if trimmed == "" {
		return ErrEmptyContent
	}
	if len([]rune(m.Content)) > MaxContentLength {
		return ErrContentTooLong
	}
	return nil
}

// ChatFilter specifies query parameters for chat history.
type ChatFilter struct {
	ClassroomID string
	UserID      string // if specified, filters whispers visible only to this user
	Limit       int
	Since       time.Time
}

type subscription struct {
	userID string
	ch     chan ChatMessage
}

// ChatManager coordinates classroom chat routing, whispering, and history retention.
type ChatManager struct {
	mu            sync.RWMutex
	history       map[string][]ChatMessage // classroomID -> messages
	subscriptions map[string][]*subscription
	maxHistory    int
}

// NewChatManager initializes a new ChatManager.
func NewChatManager() *ChatManager {
	return &ChatManager{
		history:       make(map[string][]ChatMessage),
		subscriptions: make(map[string][]*subscription),
		maxHistory:    MaxChatHistory,
	}
}

func generateMessageID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("msg-%d-%s", time.Now().UnixMilli(), hex.EncodeToString(b))
}

// SendMessage validates, stores, and routes a chat message to subscribers.
func (c *ChatManager) SendMessage(msg ChatMessage) (ChatMessage, error) {
	if err := msg.Validate(); err != nil {
		return ChatMessage{}, err
	}

	if msg.ID == "" {
		msg.ID = generateMessageID()
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	msg.IsWhisper = strings.TrimSpace(msg.TargetID) != ""

	c.mu.Lock()
	// Append to history
	hist := c.history[msg.ClassroomID]
	hist = append(hist, msg)
	if len(hist) > c.maxHistory {
		hist = hist[len(hist)-c.maxHistory:]
	}
	c.history[msg.ClassroomID] = hist

	// Collect target subscriber channels
	var targets []chan ChatMessage
	subs := c.subscriptions[msg.ClassroomID]
	for _, sub := range subs {
		if !msg.IsWhisper {
			// Public broadcast goes to everyone in the classroom
			targets = append(targets, sub.ch)
		} else {
			// Whisper is delivered ONLY to the sender and the designated recipient
			if sub.userID == msg.SenderID || sub.userID == msg.TargetID {
				targets = append(targets, sub.ch)
			}
		}
	}
	c.mu.Unlock()

	// Non-blocking deliver to collected channels
	for _, ch := range targets {
		select {
		case ch <- msg:
		default:
			// Buffer full, drop or skip to avoid blocking chat engine
		}
	}

	return msg, nil
}

// Subscribe listens for chat messages in a classroom.
// Returns a read-only channel and an unsubscribe cancel function.
func (c *ChatManager) Subscribe(classroomID, userID string) (<-chan ChatMessage, func()) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ch := make(chan ChatMessage, 64)
	sub := &subscription{
		userID: userID,
		ch:     ch,
	}

	c.subscriptions[classroomID] = append(c.subscriptions[classroomID], sub)

	cancel := func() {
		c.mu.Lock()
		defer c.mu.Unlock()

		subs := c.subscriptions[classroomID]
		for i, s := range subs {
			if s == sub {
				c.subscriptions[classroomID] = append(subs[:i], subs[i+1:]...)
				close(s.ch)
				break
			}
		}
	}

	return ch, cancel
}

// GetHistory returns filtered chat history for a classroom.
// If filter.UserID is specified, whispers not belonging to UserID are omitted for privacy.
func (c *ChatManager) GetHistory(filter ChatFilter) []ChatMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()

	raw := c.history[filter.ClassroomID]
	if len(raw) == 0 {
		return []ChatMessage{}
	}

	out := make([]ChatMessage, 0, len(raw))
	for _, m := range raw {
		if !filter.Since.IsZero() && m.Timestamp.Before(filter.Since) {
			continue
		}

		// Privacy filter: if whisper, only visible to sender or target
		if m.IsWhisper && filter.UserID != "" {
			if m.SenderID != filter.UserID && m.TargetID != filter.UserID {
				continue
			}
		}

		out = append(out, m)
	}

	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[len(out)-filter.Limit:]
	}

	return out
}
