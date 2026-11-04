package main

import (
	"encoding/json"
	"net/http"
	"time"

	"localmesh/internal/capabilities/attendance"
	"localmesh/internal/capabilities/interaction"
	"localmesh/internal/capabilities/quiz"
)

// Student-side HTTP handlers. They mirror the web console's endpoints so
// student agents (or curl simulations) can drive chat, hands, buzzer, quiz
// submissions and attendance check-ins against this console.

func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(target)
}

// handleAgentPoll is the command channel: agents POST acknowledgements and
// audit events, and receive the desired control state plus pending commands.
func (c *Console) handleAgentPoll(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		DeviceID string       `json:"device_id"`
		Acked    []string     `json:"acked"`
		Events   []AgentEvent `json:"events"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if payload.DeviceID == "" {
		http.Error(w, "device_id is required", http.StatusBadRequest)
		return
	}

	state, commands := c.poll(payload.DeviceID, payload.Acked)
	c.recordEvents(payload.DeviceID, state.ChannelID, payload.Events)

	writeJSON(w, http.StatusOK, map[string]any{
		"state":    state,
		"commands": commands,
	})
}

// handleStudentChat accepts a classroom chat message from a student.
func (c *Console) handleStudentChat(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		SenderID   string `json:"sender_id"`
		SenderName string `json:"sender_name"`
		Content    string `json:"content"`
		Target     string `json:"target"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if payload.SenderID == "" {
		payload.SenderID = "student-anon"
	}
	if payload.SenderName == "" {
		payload.SenderName = payload.SenderID
	}

	msg, err := c.chatMgr.SendMessage(interaction.ChatMessage{
		ClassroomID: defaultClassroomID,
		SenderID:    payload.SenderID,
		SenderName:  payload.SenderName,
		SenderRole:  interaction.RoleStudent,
		TargetID:    payload.Target,
		Content:     payload.Content,
		Timestamp:   time.Now().UTC(),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, msg)
}

// handleRaiseHand registers a raised hand.
func (c *Console) handleRaiseHand(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		DeviceID    string `json:"device_id"`
		StudentName string `json:"student_name"`
		Reason      string `json:"reason"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if payload.DeviceID == "" {
		http.Error(w, "device_id is required", http.StatusBadRequest)
		return
	}

	record, err := c.raiseHandQ.Raise(defaultClassroomID, payload.DeviceID, payload.StudentName, payload.Reason)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

// handleBuzzerPress registers a buzzer press.
func (c *Console) handleBuzzerPress(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		DeviceID    string `json:"device_id"`
		StudentName string `json:"student_name"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	rank, deltaMs, err := c.buzzer.Buzz(payload.DeviceID, payload.StudentName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rank": rank, "delta_ms": deltaMs})
}

// handleQuizSubmit grades and records a quiz submission.
func (c *Console) handleQuizSubmit(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	session := c.quizSession
	c.mu.Unlock()
	if session == nil {
		http.Error(w, "no active quiz session", http.StatusBadRequest)
		return
	}

	var sub quiz.Submission
	if err := decodeJSON(r, &sub); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	graded, err := session.Submit(sub)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, graded)
}

// handleAttendanceCheckIn records a student check-in.
func (c *Console) handleAttendanceCheckIn(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	sess := c.attendance
	c.mu.Unlock()
	if sess == nil {
		http.Error(w, "no active attendance session", http.StatusBadRequest)
		return
	}

	var req attendance.CheckInRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := sess.CheckIn(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "student_id": req.StudentID})
}
