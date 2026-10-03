package protocol

import (
	"errors"
	"testing"
	"time"
)

func validCommand(now time.Time) Envelope {
	return Envelope{
		ProtocolVersion: CurrentVersion,
		MessageType:     MessageCommandRequest,
		MessageID:       "msg-1",
		RequestID:       "req-1",
		SessionID:       "session-1",
		ClassroomID:     "CLASS-2026-001",
		SenderID:        "T-001",
		SentAt:          now,
		Deadline:        now.Add(time.Second),
		IdempotencyKey:  "T-001:open:group-a:1",
		Body:            []byte(`{"name":"process.launch"}`),
	}
}

func TestEnvelopeValidate(t *testing.T) {
	now := time.Unix(100, 0)
	tests := []struct {
		name string
		edit func(*Envelope)
		want error
	}{
		{name: "valid", want: nil},
		{name: "unsupported version", edit: func(e *Envelope) { e.ProtocolVersion = 2 }, want: ErrUnsupportedVersion},
		{name: "expired", edit: func(e *Envelope) { e.Deadline = now }, want: ErrExpiredMessage},
		{name: "missing command session", edit: func(e *Envelope) { e.SessionID = "" }, want: ErrInvalidEnvelope},
		{name: "invalid id", edit: func(e *Envelope) { e.MessageID = "message with spaces" }, want: ErrInvalidEnvelope},
		{name: "oversized body", edit: func(e *Envelope) { e.Body = make([]byte, MaxMessageBytes+1) }, want: ErrMessageTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := validCommand(now)
			if tt.edit != nil {
				tt.edit(&e)
			}
			if err := e.Validate(now); !errors.Is(err, tt.want) {
				t.Fatalf("Validate() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestNonCommandMessagesDoNotRequireCommandFields(t *testing.T) {
	now := time.Unix(100, 0)
	e := validCommand(now)
	e.MessageType = MessageDiscoveryAnnounce
	e.SessionID = ""
	e.ClassroomID = ""
	e.IdempotencyKey = ""
	if err := e.Validate(now); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
