# SQLite 持久化模型

## 原则

SQLite 是本地事实源，网络状态是可重建缓存。设备在线状态、发现地址和心跳不能阻塞课堂成员关系或审计写入。

## 表模型

```text
devices
  device_id PK, machine_guid UNIQUE, hostname, platform, agent_version,
  public_key, status, created_at, updated_at, revoked_at

network_interfaces
  id PK, device_id FK, name, kind, ip, subnet, gateway, observed_at

teachers
  teacher_id PK, display_name, identity_key, created_at, revoked_at

students
  student_id PK, display_name, external_ref, created_at, revoked_at

classrooms
  classroom_id PK, name, join_code_hash, state, created_at, archived_at

classroom_members
  classroom_id FK, member_id, member_kind, role, source, approved_by,
  joined_at, revoked_at, PRIMARY KEY(classroom_id, member_id)

groups
  group_id PK, classroom_id FK, name, created_at, archived_at

group_devices
  group_id FK, device_id FK, assigned_at, removed_at,
  PRIMARY KEY(group_id, device_id)

seats
  seat_id PK, classroom_id FK, label, device_id FK NULL, assigned_at, removed_at

sessions
  session_id PK, device_id FK, classroom_id FK, transport, state,
  opened_at, last_seen_at, expires_at, closed_at, close_reason

commands
  command_id PK, request_id, idempotency_key UNIQUE, classroom_id FK,
  actor_id, capability, target_kind, target_id, payload, deadline,
  state, created_at, completed_at

command_results
  command_id FK, device_id FK, state, error_code, error_summary,
  attempts, started_at, completed_at, PRIMARY KEY(command_id, device_id)

audit_events
  event_id PK, occurred_at, actor_id, classroom_id, action, target_kind,
  target_id, outcome, error_code, metadata_json
```

## 约束

- 外键启用 `PRAGMA foreign_keys = ON`。
- 所有时间使用 UTC RFC3339 或 SQLite UTC 时间值。
- `revoked_at`/`removed_at` 采用软撤销，保留审计关联。
- `payload` 只存命令参数，不存屏幕帧和大文件；大对象使用受控文件任务存储。
- `idempotency_key` 唯一约束保证重试不会创建第二个执行意图。
- 审计事件只追加，不提供更新接口。

## 事务边界

1. Pairing approve：更新成员关系、写审计事件、提交为一个事务。
2. Command create：写命令和目标快照、写审计事件、提交为一个事务。
3. Command result：写单设备结果并更新聚合状态，可独立重试。
4. Heartbeat：只更新 session/device 观测，不与命令事务串联。

## 当前实现

`internal/storage/sqlite` 提供纯 Go SQLite adapter，启动时执行嵌入式 migration，并强制启用外键。当前已实现 `commands`、`command_results` 和 `audit_events` 的写入；pairing、成员和 session 查询将在下一片持久化切片加入。
