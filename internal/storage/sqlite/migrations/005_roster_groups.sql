-- Roster, groups and seating persistence.
CREATE TABLE IF NOT EXISTS students (
    student_id TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    device_id  TEXT DEFAULT '',
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS groups (
    group_id   TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS group_members (
    group_id  TEXT NOT NULL,
    device_id TEXT NOT NULL,
    PRIMARY KEY (group_id, device_id)
);

CREATE TABLE IF NOT EXISTS seating (
    classroom_id TEXT NOT NULL,
    row          INTEGER NOT NULL,
    col          INTEGER NOT NULL,
    student_id   TEXT DEFAULT '',
    student_name TEXT DEFAULT '',
    device_id    TEXT DEFAULT '',
    updated_at   TEXT NOT NULL,
    PRIMARY KEY (classroom_id, row, col)
);
