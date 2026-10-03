package pairing

import (
	"errors"
	"strings"
	"time"

	"localmesh/internal/domain/identity"
)

var (
	ErrInvalidRequest = errors.New("invalid pairing request")
	ErrInvalidStatus  = errors.New("invalid pairing request status")
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusRevoked  Status = "revoked"
	StatusRejected Status = "rejected"
)

type Request struct {
	RequestID   string
	DeviceID    identity.DeviceID
	ClassroomID string
	Code        string
	Status      Status
	CreatedAt   time.Time
	ReviewedAt  time.Time
}

func NewRequest(requestID string, deviceID identity.DeviceID, classroomID, code string, now time.Time) (Request, error) {
	if strings.TrimSpace(requestID) == "" || deviceID == "" || strings.TrimSpace(classroomID) == "" || len(code) < 6 {
		return Request{}, ErrInvalidRequest
	}
	return Request{RequestID: requestID, DeviceID: deviceID, ClassroomID: classroomID, Code: code, Status: StatusPending, CreatedAt: now}, nil
}

func (r *Request) Approve(now time.Time) error {
	if r.Status != StatusPending {
		return ErrInvalidStatus
	}
	r.Status = StatusApproved
	r.ReviewedAt = now
	return nil
}

func (r *Request) Revoke(now time.Time) error {
	if r.Status != StatusApproved {
		return ErrInvalidStatus
	}
	r.Status = StatusRevoked
	r.ReviewedAt = now
	return nil
}
