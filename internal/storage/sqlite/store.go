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
	"localmesh/internal/domain/identity"
	"localmesh/internal/domain/pairing"
	"localmesh/internal/domain/session"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
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
	// WAL + NORMAL 同步：多读单写场景的吞吐与掉电安全折中
	if _, err := s.db.ExecContext(ctx, `PRAGMA journal_mode = WAL`); err != nil {
		return fmt.Errorf("enable wal: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA synchronous = NORMAL`); err != nil {
		return fmt.Errorf("set synchronous: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	migrations := []struct {
		version int
		name    string
	}{
		{version: 1, name: "migrations/001_initial.sql"},
		{version: 2, name: "migrations/002_runtime_state.sql"},
		{version: 3, name: "migrations/003_multiplatform_defaults.sql"},
		{version: 4, name: "migrations/004_agent_audit.sql"},
		{version: 5, name: "migrations/005_roster_groups.sql"},
	}
	for _, migration := range migrations {
		version, name := migration.version, migration.name
		var applied int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&applied); err != nil {
			return fmt.Errorf("read migration state %d: %w", version, err)
		}
		if applied > 0 {
			continue
		}
		script, err := migrationFS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration %d: %w", version, err)
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, string(script)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, version, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", version, err)
		}
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

func (s *Store) CreatePairingRequest(ctx context.Context, request pairing.Request) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO pairing_requests(request_id, device_id, classroom_id, join_code, status, created_at) VALUES(?, ?, ?, ?, ?, ?)`, request.RequestID, request.DeviceID, request.ClassroomID, request.Code, request.Status, request.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert pairing request: %w", err)
	}
	return nil
}

func (s *Store) DecidePairingRequest(ctx context.Context, requestID string, status pairing.Status, reviewedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE pairing_requests SET status = ?, reviewed_at = ? WHERE request_id = ? AND status = ?`, status, reviewedAt.UTC().Format(time.RFC3339Nano), requestID, pairing.StatusPending)
	if err != nil {
		return fmt.Errorf("update pairing request: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("pairing request %s: %w", requestID, sql.ErrNoRows)
	}
	return nil
}

func (s *Store) AddMember(ctx context.Context, classroomID, memberID, memberKind, role, source, approvedBy string, joinedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO classroom_members(classroom_id, member_id, member_kind, role, source, approved_by, joined_at) VALUES(?, ?, ?, ?, ?, ?, ?) ON CONFLICT(classroom_id, member_id) DO UPDATE SET member_kind=excluded.member_kind, role=excluded.role, source=excluded.source, approved_by=excluded.approved_by, joined_at=excluded.joined_at, revoked_at=NULL`, classroomID, memberID, memberKind, role, source, approvedBy, joinedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("add classroom member: %w", err)
	}
	return nil
}

func (s *Store) RevokeMember(ctx context.Context, classroomID, memberID string, revokedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE classroom_members SET revoked_at = ? WHERE classroom_id = ? AND member_id = ? AND revoked_at IS NULL`, revokedAt.UTC().Format(time.RFC3339Nano), classroomID, memberID)
	if err != nil {
		return fmt.Errorf("revoke classroom member: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("classroom member %s: %w", memberID, sql.ErrNoRows)
	}
	return nil
}

func (s *Store) OpenSession(ctx context.Context, value session.Session, classroomID, transport string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(session_id, device_id, classroom_id, transport, state, opened_at, last_seen_at, expires_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, value.ID, value.DeviceID, classroomID, transport, value.State, value.LastSeen.UTC().Format(time.RFC3339Nano), value.LastSeen.UTC().Format(time.RFC3339Nano), value.ExpiresAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("open session: %w", err)
	}
	return nil
}

func (s *Store) HeartbeatSession(ctx context.Context, sessionID string, lastSeen, expiresAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET state = 'active', last_seen_at = ?, expires_at = ? WHERE session_id = ? AND state != 'closed'`, lastSeen.UTC().Format(time.RFC3339Nano), expiresAt.UTC().Format(time.RFC3339Nano), sessionID)
	if err != nil {
		return fmt.Errorf("heartbeat session: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("session %s: %w", sessionID, sql.ErrNoRows)
	}
	return nil
}

func (s *Store) CloseSession(ctx context.Context, sessionID, reason string, closedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET state = 'closed', closed_at = ?, close_reason = ? WHERE session_id = ? AND state != 'closed'`, closedAt.UTC().Format(time.RFC3339Nano), reason, sessionID)
	if err != nil {
		return fmt.Errorf("close session: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("session %s: %w", sessionID, sql.ErrNoRows)
	}
	return nil
}

func parseTimestamp(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

func (s *Store) ListSessions(ctx context.Context, classroomID string) ([]application.SessionRecord, error) {
	query := `SELECT session_id, device_id, classroom_id, transport, state, opened_at, last_seen_at, expires_at FROM sessions`
	args := []any{}
	if classroomID != "" {
		query += ` WHERE classroom_id = ?`
		args = append(args, classroomID)
	}
	query += ` ORDER BY device_id, opened_at DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	result := make([]application.SessionRecord, 0)
	for rows.Next() {
		var value application.SessionRecord
		var deviceID, state, openedAt, lastSeen, expires string
		if err := rows.Scan(&value.ID, &deviceID, &value.ClassroomID, &value.Transport, &state, &openedAt, &lastSeen, &expires); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		value.DeviceID = identity.DeviceID(deviceID)
		value.State = session.State(state)
		if value.OpenedAt, err = parseTimestamp(openedAt); err != nil {
			return nil, fmt.Errorf("parse opened_at: %w", err)
		}
		if value.LastSeen, err = parseTimestamp(lastSeen); err != nil {
			return nil, fmt.Errorf("parse last_seen_at: %w", err)
		}
		if value.ExpiresAt, err = parseTimestamp(expires); err != nil {
			return nil, fmt.Errorf("parse expires_at: %w", err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	return result, nil
}

func (s *Store) FindMember(ctx context.Context, classroomID, memberID string) (application.MemberRecord, error) {
	var value application.MemberRecord
	var revoked sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT classroom_id, member_id, role, revoked_at FROM classroom_members WHERE classroom_id = ? AND member_id = ?`, classroomID, memberID).Scan(&value.ClassroomID, &value.MemberID, &value.Role, &revoked)
	if err != nil {
		return value, err
	}
	if revoked.Valid && revoked.String != "" {
		parsed, err := parseTimestamp(revoked.String)
		if err != nil {
			return value, fmt.Errorf("parse revoked_at: %w", err)
		}
		value.RevokedAt = &parsed
	}
	return value, nil
}


// Student is one roster entry.
type Student struct {
	StudentID string
	Name      string
	DeviceID  string
	UpdatedAt time.Time
}

// UpsertStudent adds or updates a roster entry.
func (s *Store) UpsertStudent(ctx context.Context, student Student) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO students(student_id, name, device_id, updated_at) VALUES(?, ?, ?, ?)
		ON CONFLICT(student_id) DO UPDATE SET name=excluded.name, device_id=excluded.device_id, updated_at=excluded.updated_at`,
		student.StudentID, student.Name, student.DeviceID, student.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

// RemoveStudent deletes a roster entry.
func (s *Store) RemoveStudent(ctx context.Context, studentID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM students WHERE student_id = ?`, studentID)
	return err
}

// ListStudents returns all roster entries ordered by name.
func (s *Store) ListStudents(ctx context.Context) ([]Student, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT student_id, name, COALESCE(device_id, ''), updated_at FROM students ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Student{}
	for rows.Next() {
		var st Student
		var updated string
		if err := rows.Scan(&st.StudentID, &st.Name, &st.DeviceID, &updated); err != nil {
			return nil, err
		}
		if st.UpdatedAt, err = parseTimestamp(updated); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// Group is one named device group.
type Group struct {
	GroupID   string
	Name      string
	UpdatedAt time.Time
}

// GroupMember is one device assignment in a group.
type GroupMember struct {
	GroupID  string
	DeviceID string
}

// UpsertGroup adds or renames a group.
func (s *Store) UpsertGroup(ctx context.Context, group Group) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO groups(group_id, name, updated_at) VALUES(?, ?, ?)
		ON CONFLICT(group_id) DO UPDATE SET name=excluded.name, updated_at=excluded.updated_at`,
		group.GroupID, group.Name, group.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

// DeleteGroup removes a group and its memberships.
func (s *Store) DeleteGroup(ctx context.Context, groupID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM group_members WHERE group_id = ?`, groupID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM groups WHERE group_id = ?`, groupID)
	return err
}

// AddGroupMember assigns a device to a group.
func (s *Store) AddGroupMember(ctx context.Context, groupID, deviceID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO group_members(group_id, device_id) VALUES(?, ?)`, groupID, deviceID)
	return err
}

// RemoveGroupMember unassigns a device from a group.
func (s *Store) RemoveGroupMember(ctx context.Context, groupID, deviceID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM group_members WHERE group_id = ? AND device_id = ?`, groupID, deviceID)
	return err
}

// ListGroups returns all groups with their member device ids.
func (s *Store) ListGroups(ctx context.Context) ([]Group, map[string][]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT group_id, name, updated_at FROM groups ORDER BY name`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	groups := []Group{}
	for rows.Next() {
		var g Group
		var updated string
		if err := rows.Scan(&g.GroupID, &g.Name, &updated); err != nil {
			return nil, nil, err
		}
		if g.UpdatedAt, err = parseTimestamp(updated); err != nil {
			return nil, nil, err
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	members := map[string][]string{}
	mrows, err := s.db.QueryContext(ctx, `SELECT group_id, device_id FROM group_members`)
	if err != nil {
		return nil, nil, err
	}
	defer mrows.Close()
	for mrows.Next() {
		var m GroupMember
		if err := mrows.Scan(&m.GroupID, &m.DeviceID); err != nil {
			return nil, nil, err
		}
		members[m.GroupID] = append(members[m.GroupID], m.DeviceID)
	}
	return groups, members, mrows.Err()
}

// SeatRecord is one persisted seat assignment.
type SeatRecord struct {
	ClassroomID string
	Row         int
	Col         int
	StudentID   string
	StudentName string
	DeviceID    string
	UpdatedAt   time.Time
}

// UpsertSeat persists one seat assignment.
func (s *Store) UpsertSeat(ctx context.Context, seat SeatRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO seating(classroom_id, row, col, student_id, student_name, device_id, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(classroom_id, row, col) DO UPDATE SET student_id=excluded.student_id, student_name=excluded.student_name, device_id=excluded.device_id, updated_at=excluded.updated_at`,
		seat.ClassroomID, seat.Row, seat.Col, seat.StudentID, seat.StudentName, seat.DeviceID, seat.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

// ClearSeat removes a persisted seat assignment.
func (s *Store) ClearSeat(ctx context.Context, classroomID string, row, col int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM seating WHERE classroom_id = ? AND row = ? AND col = ?`, classroomID, row, col)
	return err
}

// ListSeats returns all persisted seat assignments for a classroom.
func (s *Store) ListSeats(ctx context.Context, classroomID string) ([]SeatRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT classroom_id, row, col, COALESCE(student_id, ''), COALESCE(student_name, ''), COALESCE(device_id, ''), updated_at FROM seating WHERE classroom_id = ?`, classroomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SeatRecord{}
	for rows.Next() {
		var seat SeatRecord
		var updated string
		if err := rows.Scan(&seat.ClassroomID, &seat.Row, &seat.Col, &seat.StudentID, &seat.StudentName, &seat.DeviceID, &updated); err != nil {
			return nil, err
		}
		if seat.UpdatedAt, err = parseTimestamp(updated); err != nil {
			return nil, err
		}
		out = append(out, seat)
	}
	return out, rows.Err()
}

var _ application.CommandRepository = (*Store)(nil)
var _ application.AuditSink = (*Store)(nil)
var _ application.PairingRepository = (*Store)(nil)
var _ application.MembershipRepository = (*Store)(nil)
var _ application.SessionRepository = (*Store)(nil)
var _ application.SessionRecoveryRepository = (*Store)(nil)

// AgentEvent is one audit record reported by a student agent.
type AgentEvent struct {
	EventID    string
	DeviceID   string
	ChannelID  int
	Kind       string
	Detail     string
	OccurredAt time.Time
}

// AppendAgentEvent stores an agent-reported audit record.
func (s *Store) AppendAgentEvent(ctx context.Context, event AgentEvent) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO agent_events(event_id, device_id, channel_id, kind, detail, occurred_at) VALUES(?, ?, ?, ?, ?, ?)`,
		event.EventID, event.DeviceID, event.ChannelID, event.Kind, event.Detail, event.OccurredAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert agent event: %w", err)
	}
	return nil
}

// AppendAgentEvents stores a batch of agent audit records in one transaction.
func (s *Store) AppendAgentEvents(ctx context.Context, events []AgentEvent) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO agent_events(event_id, device_id, channel_id, kind, detail, occurred_at) VALUES(?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, event := range events {
		if _, err := stmt.ExecContext(ctx, event.EventID, event.DeviceID, event.ChannelID, event.Kind, event.Detail, event.OccurredAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListAgentEvents returns the most recent agent audit records, newest first.
func (s *Store) ListAgentEvents(ctx context.Context, limit int, deviceID string) ([]AgentEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT event_id, device_id, channel_id, kind, detail, occurred_at FROM agent_events`
	args := []any{}
	if deviceID != "" {
		query += ` WHERE device_id = ?`
		args = append(args, deviceID)
	}
	query += ` ORDER BY occurred_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query agent events: %w", err)
	}
	defer rows.Close()

	out := make([]AgentEvent, 0, limit)
	for rows.Next() {
		var event AgentEvent
		var occurred string
		if err := rows.Scan(&event.EventID, &event.DeviceID, &event.ChannelID, &event.Kind, &event.Detail, &occurred); err != nil {
			return nil, fmt.Errorf("scan agent event: %w", err)
		}
		parsed, err := parseTimestamp(occurred)
		if err != nil {
			return nil, err
		}
		event.OccurredAt = parsed
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent events: %w", err)
	}
	return out, nil
}
