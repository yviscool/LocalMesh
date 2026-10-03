package protocol

import (
	"errors"
	"strings"
	"time"
)

const (
	CurrentVersion  uint16 = 1
	MaxMessageBytes        = 256 * 1024
	MaxIDLength            = 128
)

var (
	ErrInvalidEnvelope    = errors.New("invalid protocol envelope")
	ErrUnsupportedVersion = errors.New("unsupported protocol version")
	ErrMessageTooLarge    = errors.New("protocol message too large")
	ErrExpiredMessage     = errors.New("protocol message deadline exceeded")
)

type MessageType string

const (
	MessageDiscoveryAnnounce MessageType = "discovery.announce"
	MessagePairingRequest    MessageType = "pairing.request"
	MessagePairingDecision   MessageType = "pairing.decision"
	MessageSessionOpen       MessageType = "session.open"
	MessageSessionHeartbeat  MessageType = "session.heartbeat"
	MessageCommandRequest    MessageType = "command.request"
	MessageCommandAccepted   MessageType = "command.accepted"
	MessageCommandResult     MessageType = "command.result"
	MessageStateSnapshot     MessageType = "state.snapshot"
)

type Envelope struct {
	ProtocolVersion uint16
	MessageType     MessageType
	MessageID       string
	RequestID       string
	SessionID       string
	ClassroomID     string
	SenderID        string
	SentAt          time.Time
	Deadline        time.Time
	IdempotencyKey  string
	Body            []byte
}

func (e Envelope) Validate(now time.Time) error {
	if e.ProtocolVersion != CurrentVersion {
		return ErrUnsupportedVersion
	}
	if !validID(e.MessageID) || !validID(e.RequestID) || !validID(e.SenderID) || e.MessageType == "" {
		return ErrInvalidEnvelope
	}
	if e.SentAt.IsZero() || e.Deadline.IsZero() || e.Deadline.Before(e.SentAt) {
		return ErrInvalidEnvelope
	}
	if !now.Before(e.Deadline) {
		return ErrExpiredMessage
	}
	if len(e.Body) > MaxMessageBytes {
		return ErrMessageTooLarge
	}
	if e.MessageType == MessageCommandRequest {
		if !validID(e.SessionID) || !validID(e.ClassroomID) || !validID(e.IdempotencyKey) {
			return ErrInvalidEnvelope
		}
	}
	return nil
}

func validID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > MaxIDLength {
		return false
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r == ':' || r == '.' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}
