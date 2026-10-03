package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	"localmesh/internal/application"
	"localmesh/internal/domain/command"
	_ "modernc.org/sqlite"
)

//go:embed migrations/001_initial.sql
var migrationFS embed.FS

var ErrDuplicateCommand = errors.New("duplicate command idempotency key")

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	store := &Store{db: db}
	if err := store.Migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("enable sqlite foreign keys: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	var applied int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = 1`).Scan(&applied); err != nil {
		return fmt.Errorf("read migration state: %w", err)
	}
	if applied > 0 {
		return nil
	}
	script, err := migrationFS.ReadFile("migrations/001_initial.sql")
	if err != nil {
		return fmt.Errorf("read initial migration: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	if _, err := tx.ExecContext(ctx, string(script)); err != nil {
		tx.Rollback()
		return fmt.Errorf("apply initial migration: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES(1, ?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		tx.Rollback()
		return fmt.Errorf("record initial migration: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}

func (s *Store) Create(ctx context.Context, value command.Envelope) error {
	payload := value.Payload
	if payload == nil {
		payload = []byte{}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO commands(command_id, request_id, idempotency_key, classroom_id, actor_id, capability, target_kind, target_id, name, payload, deadline, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, value.CommandID, value.RequestID, value.IdempotencyKey, value.ClassroomID, value.ActorID, value.Capability, value.Target.Kind, value.Target.ID, value.Name, payload, value.Deadline.UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert command: %w", err)
	}
	return nil
}

func (s *Store) RecordResult(ctx context.Context, value command.Result) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO command_results(command_id, device_id, state, error_code, error_summary, attempts, completed_at) VALUES(?, ?, ?, ?, ?, ?, ?) ON CONFLICT(command_id, device_id) DO UPDATE SET state=excluded.state, error_code=excluded.error_code, error_summary=excluded.error_summary, attempts=excluded.attempts, completed_at=excluded.completed_at`, value.CommandID, value.Target.ID, value.Status, value.Code, value.Error, 1, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert command result: %w", err)
	}
	return nil
}

func (s *Store) Append(ctx context.Context, event application.AuditEvent) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_events(event_id, occurred_at, actor_id, classroom_id, action, target_kind, target_id, outcome, error_code) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, event.EventID, event.OccurredAt.UTC().Format(time.RFC3339Nano), event.ActorID, event.ClassroomID, event.Action, event.TargetKind, event.TargetID, event.Outcome, event.ErrorCode)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

var _ application.CommandRepository = (*Store)(nil)
var _ application.AuditSink = (*Store)(nil)
