package sqlite

import (
	"context"
	"testing"
	"time"

	"localmesh/internal/application"
	"localmesh/internal/domain/command"
	"localmesh/internal/domain/identity"
	"localmesh/internal/domain/pairing"
	"localmesh/internal/domain/session"
)

func TestStoreMigratesAndPersistsCommandAndAudit(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, "file:localmesh-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.ExecContext(ctx, `INSERT INTO classrooms(classroom_id, name, created_at) VALUES('CLASS-1', 'Test', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	value, err := command.New("cmd-1", "process.launch", "launch", command.Target{Kind: command.TargetDevice, ID: "device-1"}, time.Now().Add(time.Minute), command.RetryPolicy{MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	value.RequestID, value.IdempotencyKey, value.ActorID, value.ClassroomID = "req-1", "key-1", "T-001", "CLASS-1"
	if err := store.Create(ctx, value); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(ctx, application.AuditEvent{EventID: "event-1", OccurredAt: time.Now(), ActorID: "T-001", ClassroomID: "CLASS-1", Action: "launch", TargetKind: "device", TargetID: "device-1", Outcome: "accepted"}); err != nil {
		t.Fatal(err)
	}
	var commands, audits int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM commands`).Scan(&commands); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if commands != 1 || audits != 1 {
		t.Fatalf("commands=%d audits=%d", commands, audits)
	}
}

func TestStoreEnforcesIdempotencyAndForeignKeys(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, "file:localmesh-constraints?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	value, _ := command.New("cmd-1", "process.launch", "launch", command.Target{Kind: command.TargetDevice, ID: "device-1"}, time.Now().Add(time.Minute), command.RetryPolicy{MaxAttempts: 1})
	value.RequestID, value.IdempotencyKey, value.ActorID, value.ClassroomID = "req-1", "key-1", "T-001", "missing"
	if err := store.Create(ctx, value); err == nil {
		t.Fatal("foreign key insert should fail")
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO classrooms(classroom_id, name, created_at) VALUES('CLASS-1', 'Test', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	value.ClassroomID = "CLASS-1"
	if err := store.Create(ctx, value); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, value); err == nil {
		t.Fatal("duplicate idempotency key should fail")
	}
}

func TestStorePersistsPairingMembershipAndSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, "file:localmesh-runtime?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.ExecContext(ctx, `INSERT INTO classrooms(classroom_id, name, created_at) VALUES('CLASS-1', 'Test', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO devices(device_id, hostname, created_at, updated_at) VALUES('CPC-0001-AGENT', 'pc-1', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	deviceID, err := identity.NewDeviceID("CPC-0001-AGENT")
	if err != nil {
		t.Fatal(err)
	}
	request, err := pairing.NewRequest("pair-1", deviceID, "CLASS-1", "123456", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreatePairingRequest(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := store.DecidePairingRequest(ctx, request.RequestID, pairing.StatusApproved, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.AddMember(ctx, "CLASS-1", string(deviceID), "student", "student", "pairing", "T-001", now); err != nil {
		t.Fatal(err)
	}
	value, err := session.New("session-1", deviceID, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.OpenSession(ctx, *value, "CLASS-1", "memory"); err != nil {
		t.Fatal(err)
	}
	if err := store.HeartbeatSession(ctx, value.ID, now.Add(time.Second), now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.CloseSession(ctx, value.ID, "test complete", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := store.db.QueryRowContext(ctx, `SELECT state FROM sessions WHERE session_id = 'session-1'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != string(session.StateClosed) {
		t.Fatalf("session status = %s, want %s", status, session.StateClosed)
	}
}
