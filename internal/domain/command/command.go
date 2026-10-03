package command

import (
	"errors"
	"strings"
	"time"
)

var ErrInvalidCommand = errors.New("invalid command")

type TargetKind string

const (
	TargetDevice    TargetKind = "device"
	TargetStudent   TargetKind = "student"
	TargetGroup     TargetKind = "group"
	TargetClassroom TargetKind = "classroom"
)

type Target struct {
	Kind TargetKind
	ID   string
}

type RetryPolicy struct {
	MaxAttempts int
	Backoff     time.Duration
}

type Envelope struct {
	CommandID      string
	RequestID      string
	IdempotencyKey string
	ActorID        string
	ClassroomID    string
	Capability     string
	Target         Target
	Name           string
	Payload        []byte
	Deadline       time.Time
	Retry          RetryPolicy
}

type ResultStatus string

const (
	ResultSucceeded ResultStatus = "succeeded"
	ResultFailed    ResultStatus = "failed"
	ResultTimedOut  ResultStatus = "timed_out"
)

type Result struct {
	CommandID string
	Target    Target
	Status    ResultStatus
	Code      string
	Error     string
}

func New(commandID, capability, name string, target Target, deadline time.Time, retry RetryPolicy) (Envelope, error) {
	if strings.TrimSpace(commandID) == "" || strings.TrimSpace(capability) == "" || strings.TrimSpace(name) == "" || target.ID == "" || deadline.IsZero() || retry.MaxAttempts < 1 || retry.Backoff < 0 {
		return Envelope{}, ErrInvalidCommand
	}
	return Envelope{CommandID: commandID, Capability: capability, Name: name, Target: target, Deadline: deadline, Retry: retry}, nil
}

func Aggregate(results []Result) ResultStatus {
	if len(results) == 0 {
		return ResultFailed
	}
	succeeded := 0
	for _, result := range results {
		if result.Status == ResultSucceeded {
			succeeded++
		}
	}
	switch {
	case succeeded == len(results):
		return ResultSucceeded
	case succeeded == 0:
		return ResultFailed
	default:
		return ResultStatus("partially_succeeded")
	}
}
