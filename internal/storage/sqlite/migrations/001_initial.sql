PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS classrooms (
    classroom_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL,
    archived_at TEXT
);

CREATE TABLE IF NOT EXISTS devices (
    device_id TEXT PRIMARY KEY,
    machine_guid TEXT UNIQUE,
    hostname TEXT NOT NULL DEFAULT '',
    platform TEXT NOT NULL DEFAULT 'windows',
    agent_version TEXT NOT NULL DEFAULT '',
    public_key BLOB,
    status TEXT NOT NULL DEFAULT 'unknown',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revoked_at TEXT
);

CREATE TABLE IF NOT EXISTS commands (
    command_id TEXT PRIMARY KEY,
    request_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    classroom_id TEXT NOT NULL REFERENCES classrooms(classroom_id),
    actor_id TEXT NOT NULL,
    capability TEXT NOT NULL,
    target_kind TEXT NOT NULL,
    target_id TEXT NOT NULL,
    name TEXT NOT NULL,
    payload BLOB NOT NULL,
    deadline TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'accepted',
    created_at TEXT NOT NULL,
    completed_at TEXT
);

CREATE TABLE IF NOT EXISTS command_results (
    command_id TEXT NOT NULL REFERENCES commands(command_id),
    device_id TEXT NOT NULL,
    state TEXT NOT NULL,
    error_code TEXT NOT NULL DEFAULT '',
    error_summary TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    started_at TEXT,
    completed_at TEXT,
    PRIMARY KEY(command_id, device_id)
);

CREATE TABLE IF NOT EXISTS audit_events (
    event_id TEXT PRIMARY KEY,
    occurred_at TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    classroom_id TEXT NOT NULL,
    action TEXT NOT NULL,
    target_kind TEXT NOT NULL,
    target_id TEXT NOT NULL,
    outcome TEXT NOT NULL,
    error_code TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_commands_classroom_created ON commands(classroom_id, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_classroom_occurred ON audit_events(classroom_id, occurred_at);

