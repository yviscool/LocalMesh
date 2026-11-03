-- Agent-reported audit events: policy enforcement results, blocked
-- program launches, foreground app usage samples.
CREATE TABLE IF NOT EXISTS agent_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id    TEXT NOT NULL UNIQUE,
    device_id   TEXT NOT NULL,
    channel_id  INTEGER NOT NULL DEFAULT 1,
    kind        TEXT NOT NULL,
    detail      TEXT NOT NULL DEFAULT '',
    occurred_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agent_events_time ON agent_events(occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_events_device ON agent_events(device_id, occurred_at DESC);
