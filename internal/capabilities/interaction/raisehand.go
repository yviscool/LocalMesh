package interaction

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrHandAlreadyRaised = errors.New("student has already raised their hand")
	ErrHandNotRaised     = errors.New("no active hand raised for device")
	ErrInvalidDeviceID   = errors.New("device id is required")
	ErrInvalidTeacherID  = errors.New("teacher id is required")
)

// HandStatus represents the lifecycle state of a student's raised hand.
type HandStatus string

const (
	HandStatusRaised       HandStatus = "raised"
	HandStatusAcknowledged HandStatus = "acknowledged"
	HandStatusCleared      HandStatus = "cleared"
)

// HandRecord describes a student's hand raise request and its handling state.
type HandRecord struct {
	ID             string     `json:"id"`
	ClassroomID    string     `json:"classroom_id"`
	DeviceID       string     `json:"device_id"`
	StudentName    string     `json:"student_name"`
	Reason         string     `json:"reason,omitempty"`
	Status         HandStatus `json:"status"`
	RaisedAt       time.Time  `json:"raised_at"`
	AcknowledgedAt time.Time  `json:"acknowledged_at,omitempty"`
	ClearedAt      time.Time  `json:"cleared_at,omitempty"`
	AcknowledgedBy string     `json:"acknowledged_by,omitempty"`
}

// RaiseHandQueue manages the raise-hand queue, transitions, and notifications.
type RaiseHandQueue struct {
	mu            sync.RWMutex
	active        map[string]map[string]*HandRecord // classroomID -> deviceID -> record
	subscriptions map[string][]chan HandRecord      // classroomID -> subscriber channels
}

// NewRaiseHandQueue initializes an empty RaiseHandQueue.
func NewRaiseHandQueue() *RaiseHandQueue {
	return &RaiseHandQueue{
		active:        make(map[string]map[string]*HandRecord),
		subscriptions: make(map[string][]chan HandRecord),
	}
}

// Raise records or updates a student hand raise.
func (q *RaiseHandQueue) Raise(classroomID, deviceID, studentName, reason string) (*HandRecord, error) {
	if strings.TrimSpace(classroomID) == "" {
		return nil, ErrClassroomIDMissing
	}
	if strings.TrimSpace(deviceID) == "" {
		return nil, ErrInvalidDeviceID
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	roomMap, ok := q.active[classroomID]
	if !ok {
		roomMap = make(map[string]*HandRecord)
		q.active[classroomID] = roomMap
	}

	existing, found := roomMap[deviceID]
	if found && existing.Status == HandStatusRaised {
		// Update reason and return existing
		existing.Reason = reason
		return existing, nil
	}

	record := &HandRecord{
		ID:          fmt.Sprintf("hand-%d-%s", time.Now().UnixMilli(), deviceID),
		ClassroomID: classroomID,
		DeviceID:    deviceID,
		StudentName: studentName,
		Reason:      reason,
		Status:      HandStatusRaised,
		RaisedAt:    time.Now(),
	}

	roomMap[deviceID] = record
	q.notifySubscribersLocked(classroomID, *record)
	return record, nil
}

// Acknowledge marks an active raised hand as acknowledged by a teacher.
func (q *RaiseHandQueue) Acknowledge(classroomID, deviceID, teacherID string) (*HandRecord, error) {
	if strings.TrimSpace(teacherID) == "" {
		return nil, ErrInvalidTeacherID
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	roomMap, ok := q.active[classroomID]
	if !ok {
		return nil, ErrHandNotRaised
	}

	record, found := roomMap[deviceID]
	if !found || record.Status != HandStatusRaised {
		return nil, ErrHandNotRaised
	}

	record.Status = HandStatusAcknowledged
	record.AcknowledgedAt = time.Now()
	record.AcknowledgedBy = teacherID

	q.notifySubscribersLocked(classroomID, *record)
	return record, nil
}

// Clear removes a student's hand raise request.
func (q *RaiseHandQueue) Clear(classroomID, deviceID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	roomMap, ok := q.active[classroomID]
	if !ok {
		return ErrHandNotRaised
	}

	record, found := roomMap[deviceID]
	if !found {
		return ErrHandNotRaised
	}

	record.Status = HandStatusCleared
	record.ClearedAt = time.Now()
	delete(roomMap, deviceID)

	q.notifySubscribersLocked(classroomID, *record)
	return nil
}

// ClearAll clears all active hands in the specified classroom.
func (q *RaiseHandQueue) ClearAll(classroomID string) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	roomMap, ok := q.active[classroomID]
	if !ok {
		return 0
	}

	now := time.Now()
	cleared := 0
	for deviceID, record := range roomMap {
		record.Status = HandStatusCleared
		record.ClearedAt = now
		q.notifySubscribersLocked(classroomID, *record)
		delete(roomMap, deviceID)
		cleared++
	}
	return cleared
}

// ListActive returns all active raised/acknowledged hands ordered chronologically (FIFO).
func (q *RaiseHandQueue) ListActive(classroomID string) []*HandRecord {
	q.mu.RLock()
	defer q.mu.RUnlock()

	roomMap := q.active[classroomID]
	if len(roomMap) == 0 {
		return []*HandRecord{}
	}

	out := make([]*HandRecord, 0, len(roomMap))
	for _, rec := range roomMap {
		out = append(out, rec)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].RaisedAt.Before(out[j].RaisedAt)
	})

	return out
}

// GetActive returns the active hand record for a device if present.
func (q *RaiseHandQueue) GetActive(classroomID, deviceID string) (*HandRecord, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	roomMap, ok := q.active[classroomID]
	if !ok {
		return nil, false
	}
	rec, ok := roomMap[deviceID]
	return rec, ok
}

// ActiveCount returns the number of active hand raises in the classroom.
func (q *RaiseHandQueue) ActiveCount(classroomID string) int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.active[classroomID])
}

// Subscribe listens for hand raise events in a classroom.
func (q *RaiseHandQueue) Subscribe(classroomID string) (<-chan HandRecord, func()) {
	q.mu.Lock()
	defer q.mu.Unlock()

	ch := make(chan HandRecord, 32)
	q.subscriptions[classroomID] = append(q.subscriptions[classroomID], ch)

	cancel := func() {
		q.mu.Lock()
		defer q.mu.Unlock()

		subs := q.subscriptions[classroomID]
		for i, subCh := range subs {
			if subCh == ch {
				q.subscriptions[classroomID] = append(subs[:i], subs[i+1:]...)
				close(subCh)
				break
			}
		}
	}

	return ch, cancel
}

func (q *RaiseHandQueue) notifySubscribersLocked(classroomID string, record HandRecord) {
	subs := q.subscriptions[classroomID]
	for _, ch := range subs {
		select {
		case ch <- record:
		default:
		}
	}
}
