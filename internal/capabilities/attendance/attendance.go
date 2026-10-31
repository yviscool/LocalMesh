package attendance

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrSessionNotPending indicates the session is not in pending state.
	ErrSessionNotPending = errors.New("attendance session is not in pending state")
	// ErrSessionNotActive indicates the session is not currently active.
	ErrSessionNotActive = errors.New("attendance session is not active")
	// ErrSessionClosed indicates the attendance session is closed.
	ErrSessionClosed = errors.New("attendance session has ended")
	// ErrAttendanceTimeout indicates the check-in time limit has expired.
	ErrAttendanceTimeout = errors.New("attendance check-in time limit expired")
	// ErrDuplicateCheckIn indicates the student has already checked in.
	ErrDuplicateCheckIn = errors.New("student has already checked in")
	// ErrDeviceAlreadyUsed indicates the device has already been used to check in.
	ErrDeviceAlreadyUsed = errors.New("device has already been used for check-in")
	// ErrInvalidStudentID indicates missing student ID.
	ErrInvalidStudentID = errors.New("student id is required")
	// ErrInvalidDeviceID indicates missing device ID.
	ErrInvalidDeviceID = errors.New("device id is required")
	// ErrInvalidClassroomID indicates missing classroom ID.
	ErrInvalidClassroomID = errors.New("classroom id is required")
	// ErrInvalidSessionTitle indicates missing session title.
	ErrInvalidSessionTitle = errors.New("session title is required")
	// ErrSessionNotFound indicates session was not found.
	ErrSessionNotFound = errors.New("attendance session not found")
	// ErrInvalidSeatPosition indicates seat row or col is negative.
	ErrInvalidSeatPosition = errors.New("invalid seat coordinates")
)

// SessionState denotes the current state of an attendance session.
type SessionState string

const (
	SessionPending SessionState = "Pending"
	SessionActive  SessionState = "Active"
	SessionClosed  SessionState = "Closed"
)

// ExpectedStudent represents a student expected to attend.
type ExpectedStudent struct {
	StudentID   string `json:"student_id"`
	StudentName string `json:"student_name"`
}

// CheckInRecord contains details of a successful student check-in.
type CheckInRecord struct {
	StudentID   string    `json:"student_id"`
	DeviceID    string    `json:"device_id"`
	StudentName string    `json:"student_name"`
	CheckedInAt time.Time `json:"checked_in_at"`
	SeatRow     int       `json:"seat_row"`
	SeatCol     int       `json:"seat_col"`
	Late        bool      `json:"late"`
}

// CheckInRequest encapsulates check-in payload from student terminal.
type CheckInRequest struct {
	StudentID   string `json:"student_id"`
	DeviceID    string `json:"device_id"`
	StudentName string `json:"student_name"`
	SeatRow     int    `json:"seat_row"`
	SeatCol     int    `json:"seat_col"`
}

// AttendanceStats summarizes the attendance session.
type AttendanceStats struct {
	TotalExpected     int      `json:"total_expected"`
	TotalCheckedIn    int      `json:"total_checked_in"`
	AttendanceRate    float64  `json:"attendance_rate"`
	UncheckedStudents []string `json:"unchecked_students"`
	LateStudents      []string `json:"late_students"`
}

// AttendanceSession manages roll-call state and check-in records.
type AttendanceSession struct {
	mu               sync.RWMutex
	ID               string                     `json:"id"`
	ClassroomID      string                     `json:"classroom_id"`
	Title            string                     `json:"title"`
	TimeLimitSeconds int                        `json:"time_limit_seconds"`
	LateThresholdSec int                        `json:"late_threshold_sec,omitempty"`
	State            SessionState               `json:"state"`
	CreatedAt        time.Time                  `json:"created_at"`
	StartedAt        time.Time                  `json:"started_at"`
	ClosedAt         time.Time                  `json:"closed_at"`
	roster           map[string]ExpectedStudent // student_id -> student
	records          map[string]*CheckInRecord  // student_id -> record
	deviceMap        map[string]string          // device_id -> student_id
}

// NewAttendanceSession constructs a new attendance session in Pending state.
func NewAttendanceSession(id, classroomID, title string, timeLimitSeconds int, roster []ExpectedStudent) (*AttendanceSession, error) {
	if strings.TrimSpace(classroomID) == "" {
		return nil, ErrInvalidClassroomID
	}
	if strings.TrimSpace(title) == "" {
		return nil, ErrInvalidSessionTitle
	}

	rosterMap := make(map[string]ExpectedStudent, len(roster))
	for _, st := range roster {
		sid := strings.TrimSpace(st.StudentID)
		if sid != "" {
			rosterMap[sid] = ExpectedStudent{
				StudentID:   sid,
				StudentName: strings.TrimSpace(st.StudentName),
			}
		}
	}

	return &AttendanceSession{
		ID:               id,
		ClassroomID:      classroomID,
		Title:            title,
		TimeLimitSeconds: timeLimitSeconds,
		State:            SessionPending,
		CreatedAt:        time.Now().UTC(),
		roster:           rosterMap,
		records:          make(map[string]*CheckInRecord),
		deviceMap:        make(map[string]string),
	}, nil
}

// Start begins roll-call, transitioning from Pending to Active.
func (s *AttendanceSession) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State != SessionPending {
		return ErrSessionNotPending
	}

	s.State = SessionActive
	s.StartedAt = time.Now().UTC()
	return nil
}

// Close explicitly ends the attendance session.
func (s *AttendanceSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State == SessionClosed {
		return ErrSessionClosed
	}

	s.State = SessionClosed
	s.ClosedAt = time.Now().UTC()
	return nil
}

// GetState returns the current state, checking whether the session has timed out.
func (s *AttendanceSession) GetState() SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()

	_ = s.isExpiredLocked(time.Now().UTC())
	return s.State
}

// RemainingSeconds returns remaining seconds for active session or 0.
func (s *AttendanceSession) RemainingSeconds() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.TimeLimitSeconds <= 0 || s.StartedAt.IsZero() || s.State != SessionActive {
		return 0
	}
	elapsed := int(time.Since(s.StartedAt).Seconds())
	remaining := s.TimeLimitSeconds - elapsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// SetLateThreshold sets the number of seconds after Start when check-in is considered late.
func (s *AttendanceSession) SetLateThreshold(seconds int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LateThresholdSec = seconds
}

// CheckIn records a student check-in, defending against duplicate entries and timeouts.
func (s *AttendanceSession) CheckIn(req CheckInRequest) error {
	req.StudentID = strings.TrimSpace(req.StudentID)
	req.DeviceID = strings.TrimSpace(req.DeviceID)
	req.StudentName = strings.TrimSpace(req.StudentName)

	if req.StudentID == "" {
		return ErrInvalidStudentID
	}
	if req.DeviceID == "" {
		return ErrInvalidDeviceID
	}
	if req.SeatRow < 0 || req.SeatCol < 0 {
		return ErrInvalidSeatPosition
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if s.isExpiredLocked(now) {
		return ErrAttendanceTimeout
	}

	switch s.State {
	case SessionPending:
		return ErrSessionNotActive
	case SessionClosed:
		return ErrSessionClosed
	case SessionActive:
		// Active, proceed
	}

	// Defend against duplicate check-in by StudentID
	if _, exists := s.records[req.StudentID]; exists {
		return ErrDuplicateCheckIn
	}

	// Defend against duplicate check-in by DeviceID
	if _, exists := s.deviceMap[req.DeviceID]; exists {
		return ErrDeviceAlreadyUsed
	}

	studentName := req.StudentName
	if studentName == "" {
		if st, ok := s.roster[req.StudentID]; ok && st.StudentName != "" {
			studentName = st.StudentName
		} else {
			studentName = req.StudentID
		}
	}

	isLate := false
	if s.LateThresholdSec > 0 && !s.StartedAt.IsZero() {
		lateDeadline := s.StartedAt.Add(time.Duration(s.LateThresholdSec) * time.Second)
		if now.After(lateDeadline) {
			isLate = true
		}
	}

	record := &CheckInRecord{
		StudentID:   req.StudentID,
		DeviceID:    req.DeviceID,
		StudentName: studentName,
		CheckedInAt: now,
		SeatRow:     req.SeatRow,
		SeatCol:     req.SeatCol,
		Late:        isLate,
	}

	s.records[req.StudentID] = record
	s.deviceMap[req.DeviceID] = req.StudentID

	return nil
}

// GetStats compiles attendance counts, rate percentage, and unchecked/late lists.
func (s *AttendanceSession) GetStats() AttendanceStats {
	s.mu.Lock()
	_ = s.isExpiredLocked(time.Now().UTC())
	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()

	totalCheckedIn := len(s.records)
	totalExpected := len(s.roster)
	if totalExpected < totalCheckedIn {
		totalExpected = totalCheckedIn
	}

	var rate float64
	if totalExpected > 0 {
		rate = math.Round((float64(totalCheckedIn)/float64(totalExpected)*100.0)*100) / 100
	}

	var unchecked []string
	for sid, st := range s.roster {
		if _, ok := s.records[sid]; !ok {
			name := st.StudentName
			if name == "" {
				name = sid
			}
			unchecked = append(unchecked, fmt.Sprintf("%s (%s)", name, sid))
		}
	}
	sort.Strings(unchecked)

	var lateStudents []string
	for _, rec := range s.records {
		if rec.Late {
			name := rec.StudentName
			if name == "" {
				name = rec.StudentID
			}
			lateStudents = append(lateStudents, fmt.Sprintf("%s (%s)", name, rec.StudentID))
		}
	}
	sort.Strings(lateStudents)

	return AttendanceStats{
		TotalExpected:     totalExpected,
		TotalCheckedIn:    totalCheckedIn,
		AttendanceRate:    rate,
		UncheckedStudents: unchecked,
		LateStudents:      lateStudents,
	}
}

// ExportCSV generates a standard CSV file with a UTF-8 BOM for Microsoft Excel compatibility.
func (s *AttendanceSession) ExportCSV() ([]byte, error) {
	s.mu.Lock()
	_ = s.isExpiredLocked(time.Now().UTC())
	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()

	var buf bytes.Buffer
	// UTF-8 BOM: \xEF\xBB\xBF
	buf.WriteString("\xEF\xBB\xBF")

	writer := csv.NewWriter(&buf)

	// CSV Header
	header := []string{
		"序号",
		"学号",
		"姓名",
		"设备ID",
		"座位行",
		"座位列",
		"签到时间",
		"状态",
	}
	if err := writer.Write(header); err != nil {
		return nil, err
	}

	// Sort checked-in records deterministically by StudentID
	recordsList := make([]*CheckInRecord, 0, len(s.records))
	for _, rec := range s.records {
		recordsList = append(recordsList, rec)
	}
	sort.Slice(recordsList, func(i, j int) bool {
		return recordsList[i].StudentID < recordsList[j].StudentID
	})

	index := 1
	for _, rec := range recordsList {
		status := "已签到"
		if rec.Late {
			status = "迟到"
		}
		row := []string{
			strconv.Itoa(index),
			rec.StudentID,
			rec.StudentName,
			rec.DeviceID,
			strconv.Itoa(rec.SeatRow),
			strconv.Itoa(rec.SeatCol),
			rec.CheckedInAt.Format("2006-01-02 15:04:05"),
			status,
		}
		if err := writer.Write(row); err != nil {
			return nil, err
		}
		index++
	}

	// Sort absent students deterministically by StudentID
	var absentList []ExpectedStudent
	for sid, st := range s.roster {
		if _, ok := s.records[sid]; !ok {
			absentList = append(absentList, st)
		}
	}
	sort.Slice(absentList, func(i, j int) bool {
		return absentList[i].StudentID < absentList[j].StudentID
	})

	for _, st := range absentList {
		row := []string{
			strconv.Itoa(index),
			st.StudentID,
			st.StudentName,
			"-",
			"-",
			"-",
			"-",
			"缺勤",
		}
		if err := writer.Write(row); err != nil {
			return nil, err
		}
		index++
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// GetRecords returns a snapshot slice of all check-in records.
func (s *AttendanceSession) GetRecords() []CheckInRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]CheckInRecord, 0, len(s.records))
	for _, rec := range s.records {
		out = append(out, *rec)
	}
	return out
}

func (s *AttendanceSession) isExpiredLocked(now time.Time) bool {
	if s.State != SessionActive {
		return false
	}
	if s.TimeLimitSeconds > 0 && !s.StartedAt.IsZero() {
		deadline := s.StartedAt.Add(time.Duration(s.TimeLimitSeconds) * time.Second)
		if now.After(deadline) {
			s.State = SessionClosed
			s.ClosedAt = deadline
			return true
		}
	}
	return false
}

// Manager manages classroom attendance sessions.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*AttendanceSession
}

// NewManager creates a Manager instance.
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*AttendanceSession),
	}
}

// CreateSession initializes and registers a new attendance session.
func (m *Manager) CreateSession(id, classroomID, title string, timeLimitSeconds int, roster []ExpectedStudent) (*AttendanceSession, error) {
	if strings.TrimSpace(id) == "" {
		id = fmt.Sprintf("att_%d", time.Now().UnixNano())
	}

	session, err := NewAttendanceSession(id, classroomID, title, timeLimitSeconds, roster)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.sessions[id] = session
	return session, nil
}

// GetSession retrieves an existing attendance session by ID.
func (m *Manager) GetSession(id string) (*AttendanceSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return session, nil
}

// ListSessions returns all sessions for the specified classroomID (or all if empty).
func (m *Manager) ListSessions(classroomID string) []*AttendanceSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var list []*AttendanceSession
	for _, s := range m.sessions {
		if classroomID == "" || s.ClassroomID == classroomID {
			list = append(list, s)
		}
	}
	return list
}

// CloseSession closes an active attendance session.
func (m *Manager) CloseSession(id string) error {
	m.mu.RLock()
	session, ok := m.sessions[id]
	m.mu.RUnlock()

	if !ok {
		return ErrSessionNotFound
	}
	return session.Close()
}
