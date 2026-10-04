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
    platform TEXT NOT NULL DEFAULT 'unknown',
    agent_version TEXT NOT NULL DEFAULT '',
    public_key BLOB,
    status TEXT NOT NULL DEFAULT 'unknown',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revoked_at TEXT
);

CREATE TABLE IF NOT EXISTS classroom_members (
    classroom_id TEXT NOT NULL REFERENCES classrooms(classroom_id),
    member_id TEXT NOT NULL,
    member_kind TEXT NOT NULL,
    role TEXT NOT NULL,
    source TEXT NOT NULL,
    approved_by TEXT NOT NULL DEFAULT '',
    joined_at TEXT NOT NULL,
    revoked_at TEXT,
    PRIMARY KEY(classroom_id, member_id)
);

CREATE TABLE IF NOT EXISTS pairing_requests (
    request_id TEXT PRIMARY KEY,
    device_id TEXT NOT NULL REFERENCES devices(device_id),
    classroom_id TEXT NOT NULL REFERENCES classrooms(classroom_id),
    join_code TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    reviewed_at TEXT
);

CREATE TABLE IF NOT EXISTS sessions (
    session_id TEXT PRIMARY KEY,
    device_id TEXT NOT NULL REFERENCES devices(device_id),
    classroom_id TEXT NOT NULL REFERENCES classrooms(classroom_id),
    transport TEXT NOT NULL,
    state TEXT NOT NULL,
    opened_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    closed_at TEXT,
    close_reason TEXT NOT NULL DEFAULT ''
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
CREATE INDEX IF NOT EXISTS idx_sessions_device_state ON sessions(device_id, state);
CREATE INDEX IF NOT EXISTS idx_pairing_classroom_status ON pairing_requests(classroom_id, status);
