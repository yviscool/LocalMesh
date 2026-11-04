package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// httpTimeout bounds all teacher API calls from the student UI.
const httpTimeout = 3 * time.Second

// postTeacherJSON POSTs a JSON body to the teacher console and returns the
// response body.
func postTeacherJSON(url string, body any, timeout time.Duration) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("teacher returned %d: %s", resp.StatusCode, string(raw))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// getTeacherJSON GETs a JSON document from the teacher console.
func getTeacherJSON(url string, out any, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("teacher returned %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

// StudentService is the binding surface for the student pages.
type StudentService struct{ ui *UI }

// Identity describes this student machine.
type Identity struct {
	DeviceID   string `json:"device_id"`
	TeacherURL string `json:"teacher_url"`
}

// Identity returns the local student identity.
func (s *StudentService) Identity(_ context.Context) Identity {
	return Identity{DeviceID: s.ui.deviceID, TeacherURL: s.ui.teacherURL}
}

// RaiseHand raises the student's hand.
func (s *StudentService) RaiseHand(_ context.Context, reason string) error {
	return s.ui.raiseHand(reason)
}

// ChatMessage mirrors interaction.ChatMessage.
type ChatMessage struct {
	ID          string `json:"id"`
	SenderName  string `json:"sender_name"`
	SenderRole  string `json:"sender_role"`
	TargetID    string `json:"target_id"`
	Content     string `json:"content"`
	Timestamp   string `json:"timestamp"`
	IsWhisper   bool   `json:"is_whisper"`
}

// RecentChat returns recent classroom messages addressed to everyone, to
// this device, or to any group this device belongs to.
func (s *StudentService) RecentChat(_ context.Context, limit int) ([]ChatMessage, error) {
	var all []ChatMessage
	if err := getTeacherJSON(s.ui.teacherURL+"/api/v1/chat/messages", &all, httpTimeout); err != nil {
		return nil, err
	}
	myGroups := map[string]bool{}
	var candidates []struct {
		GroupID string `json:"group_id"`
		Members []string `json:"members"`
	}
	_ = getTeacherJSON(s.ui.teacherURL+"/api/v1/groups", &candidates, httpTimeout)
	for _, g := range candidates {
		for _, m := range g.Members {
			if m == s.ui.deviceID {
				myGroups["group:"+g.GroupID] = true
			}
		}
	}
	out := make([]ChatMessage, 0, limit)
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		m := all[i]
		if m.TargetID == "" || m.TargetID == s.ui.deviceID || myGroups[m.TargetID] {
			out = append(out, m)
		}
	}
	return out, nil
}

// ActiveQuiz mirrors the teacher's quiz status.
type ActiveQuiz struct {
	Active     bool   `json:"active"`
	State      string `json:"state"`
	TimeRemain int    `json:"time_remain"`
	Quiz       *struct {
		ID               string `json:"id"`
		Title            string `json:"title"`
		TimeLimitSeconds int    `json:"time_limit_seconds"`
		Questions        []struct {
			ID      string   `json:"id"`
			Text    string   `json:"text"`
			Type    string   `json:"type"`
			Options []string `json:"options"`
			Points  int      `json:"points"`
		} `json:"questions"`
	} `json:"quiz"`
}

// SendChat sends a classroom chat message from this student.
func (s *StudentService) SendChat(_ context.Context, content string) error {
	body := map[string]any{
		"sender_id":   s.ui.deviceID,
		"sender_name": s.ui.student,
		"content":     content,
	}
	_, err := postTeacherJSON(s.ui.teacherURL+"/api/v1/chat/messages", body, httpTimeout)
	return err
}

// ActiveQuiz returns the current quiz for the answering dialog.
func (s *StudentService) ActiveQuiz(_ context.Context) (ActiveQuiz, error) {
	var status ActiveQuiz
	err := getTeacherJSON(s.ui.teacherURL+"/api/v1/quiz/active", &status, httpTimeout)
	return status, err
}

// SubmitQuiz submits this student's answers.
func (s *StudentService) SubmitQuiz(_ context.Context, quizID string, answers map[string][]int) (map[string]any, error) {
	body := map[string]any{
		"student_id":   s.ui.deviceID,
		"device_id":    s.ui.deviceID,
		"student_name": s.ui.student,
		"answers":      answers,
	}
	raw, err := postTeacherJSON(s.ui.teacherURL+"/api/v1/quiz/submit", body, httpTimeout)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// FocusBar brings the floating bar above the fullscreen viewer (quiz/chat).
func (s *StudentService) FocusBar(_ context.Context) {
	if s.ui.bar != nil {
		s.ui.bar.Focus()
	}
}

// Hands lists the active raise-hand records (for the raised-feedback loop).
func (s *StudentService) Hands(_ context.Context) ([]map[string]any, error) {
	var out []map[string]any
	err := getTeacherJSON(s.ui.teacherURL+"/api/v1/raisehand", &out, httpTimeout)
	return out, err
}

// BroadcastStreamURL is the local MJPEG endpoint served by the agent.
func (s *StudentService) BroadcastStreamURL(_ context.Context) string {
	// student-ui 与 agent 同机，直接吃本地流
	return "http://127.0.0.1:7450/broadcast.mjpg"
}
