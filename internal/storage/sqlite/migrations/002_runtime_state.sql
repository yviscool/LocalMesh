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
CREATE INDEX IF NOT EXISTS idx_sessions_device_state ON sessions(device_id, state);
CREATE INDEX IF NOT EXISTS idx_pairing_classroom_status ON pairing_requests(classroom_id, status);

