# LocalMesh: JiYu (极域) Replica Phase 2 - Advanced Capabilities & Anti-Tamper Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the advanced tier of Mythware JiYu (极域电子教室) capabilities in LocalMesh: high-fps multicast screen broadcasting & student demo, dual-process anti-tamper watchdog, live screen annotation/whiteboard, batch silent script execution, hardware/software inventory telemetry, and roll-call attendance.

**Architecture Alignment:**
1. **Hexagonal Domain Isolation:** All domain types remain OS-neutral. Platform hooks interface through `internal/platform/`.
2. **Worker Isolation Model (from Veyon):** Screen broadcast decoding, anti-tamper watchdog, and silent script runners run in isolated worker processes.
3. **Magpie Lightweight Native Stack:** Win32 DACL security attributes, transparent overlay rendering, and GDI memory optimization.
4. **Verification Gate:** Must pass `go test -race -shuffle=on -count=1 -timeout=5m ./...`, `go vet ./...`, `gofmt -l .`, and `git diff --check`.

---

## Capabilities & Work Breakdown

```text
Phase 1: High-Performance Screen Broadcast & Student Demo (internal/capabilities/broadcast)
Phase 2: Dual-Process Watchdog & Anti-Tamper Armor (internal/capabilities/watchdog)
Phase 3: Screen Annotation & Collaborative Whiteboard (internal/capabilities/whiteboard)
Phase 4: Remote Script & Silent Shell Execution (internal/capabilities/remoteshell)
Phase 5: Hardware/Software Asset Inventory & Telemetry (internal/capabilities/inventory)
Phase 6: Attendance, Roll-Call & Seating Synchronization (internal/capabilities/attendance)
Phase 7: Teacher Web Console Integration, End-to-End Verification & Build Delivery
```

---

## Detailed Task Breakdown

### Phase 1: High-Performance Screen Broadcast & Student Demo

**Files:**
- Create: `internal/capabilities/broadcast/frame.go`
- Create: `internal/capabilities/broadcast/multicast.go`
- Create: `internal/capabilities/broadcast/sender.go`
- Create: `internal/capabilities/broadcast/receiver.go`
- Create: `internal/capabilities/broadcast/broadcast_test.go`

**Key Components:**
- `ScreenFrame`: Sequence number, timestamp, bounds, tile dirty rectangles, JPEG/LZ4 compressed payload.
- `MulticastSender`: LAN UDP Multicast broadcaster (e.g. `239.255.42.99:8899`) with packet fragmenter/reassembler (MTU 1400 bytes, forward error correction header).
- `StudentDemo`: Dynamic role swap allowing a student device ID to act as stream sender while teacher and other students receive.
- Tests: Test packet fragmentation and reassembly, frame loss tolerance, and concurrency race conditions.

- [ ] Task 1.1: Frame protocol & packet chunker/reassembler.
- [ ] Task 1.2: UDP Multicast sender & receiver implementation.
- [ ] Task 1.3: Teacher broadcast coordinator with dynamic demo mode.
- [ ] Task 1.4: Unit tests with table-driven fragmentation checks.

---

### Phase 2: Dual-Process Watchdog & Anti-Tamper Armor

**Files:**
- Create: `internal/capabilities/watchdog/watchdog.go`
- Create: `internal/capabilities/watchdog/watchdog_windows.go`
- Create: `internal/capabilities/watchdog/watchdog_other.go`
- Create: `internal/capabilities/watchdog/watchdog_test.go`
- Reference: `C:\Users\Administrator\Desktop\Forge\upstream\magpie`, Veyon service supervisor

**Key Components:**
- Dual-process supervisor: Watchdog process continuously monitors Agent PID via Win32 `OpenProcess` with `SYNCHRONIZE` / `WaitForSingleObject`. If terminated, restarts Agent in <200ms.
- Process DACL Hardening (`watchdog_windows.go`): Modifies process token security descriptor to deny `PROCESS_TERMINATE` and `PROCESS_SUSPEND_RESUME` from non-elevated callers.
- Taskmgr & Killer Scanner: Detects malicious termination utilities (`taskmgr.exe`, `jiyutrainer.exe`, `pchunter.exe`) and preempts student tampering.

- [ ] Task 2.1: Implement cross-platform supervisor state machine.
- [ ] Task 2.2: Implement Win32 process DACL restriction and termination handler.
- [ ] Task 2.3: Implement watchdog test suite covering crash and automatic restart.

---

### Phase 3: Screen Annotation & Collaborative Whiteboard

**Files:**
- Create: `internal/capabilities/whiteboard/draw.go`
- Create: `internal/capabilities/whiteboard/whiteboard.go`
- Create: `internal/capabilities/whiteboard/whiteboard_test.go`

**Key Components:**
- Vector primitives: `DrawCommand` (Pen, Highlighter, Arrow, Rect, Ellipse, Text, Clear, Undo).
- Coordinate normalization: 0..1 float coordinate mapping to support heterogeneous student resolutions.
- Broadcast layer blending: Transmits vector commands concurrently alongside broadcast frames with sub-millisecond sync.

- [ ] Task 3.1: Define drawing primitives, serializable vectors, and canvas state machine.
- [ ] Task 3.2: Whiteboard manager supporting undo, redo, clear, and real-time command streaming.
- [ ] Task 3.3: Unit tests verifying command serialization and state reconstruction.

---

### Phase 4: Remote Script & Silent Shell Execution

**Files:**
- Create: `internal/capabilities/remoteshell/script.go`
- Create: `internal/capabilities/remoteshell/runner.go`
- Create: `internal/capabilities/remoteshell/runner_test.go`

**Key Components:**
- Supported script types: `ScriptBatch` (.bat/.cmd), `ScriptPowerShell` (.ps1), `ScriptShell` (.sh).
- Execution control: Timeout deadlines, working directory isolation, cancellable context, max output buffer limit (e.g. 1MB).
- Security audit: Mandatory audit logging (`AuditScriptExecuted`, `AuditScriptTerminated`) with command hash.

- [ ] Task 4.1: Script models and execution parameter validation.
- [ ] Task 4.2: Sandboxed process runner capturing stdout/stderr with cancellation support.
- [ ] Task 4.3: Table-driven tests with timeout and error handling checks.

---

### Phase 5: Hardware/Software Asset Inventory & Telemetry

**Files:**
- Create: `internal/capabilities/inventory/inventory.go`
- Create: `internal/capabilities/inventory/collector_windows.go`
- Create: `internal/capabilities/inventory/collector_other.go`
- Create: `internal/capabilities/inventory/inventory_test.go`

**Key Components:**
- Hardware metrics: CPU model/cores, total/available RAM, disk drives with free space, primary GPU name, MAC and IP addresses.
- System metrics: OS version, kernel build, uptime, active logged-on username.
- Software list: Queries Windows Uninstall Registry (`Software\Microsoft\Windows\CurrentVersion\Uninstall`) for installed apps and versions.

- [ ] Task 5.1: Inventory schema and system data model.
- [ ] Task 5.2: Win32 registry and system metrics collector.
- [ ] Task 5.3: Unit tests verifying telemetry format and mock collector.

---

### Phase 6: Attendance, Roll-Call & Seating Synchronization

**Files:**
- Create: `internal/capabilities/attendance/attendance.go`
- Create: `internal/capabilities/attendance/attendance_test.go`

**Key Components:**
- Roll-call session: Duration, title, state (`Pending`, `Active`, `Closed`).
- Check-in action: Student confirms attendance, records device ID, student name, and check-in timestamp.
- Integration: Synchronizes check-in state to `SeatingChart` grid (seat color turns green upon check-in); CSV report exporter.

- [ ] Task 6.1: Attendance session state machine and validation.
- [ ] Task 6.2: Check-in processing and CSV report generation.
- [ ] Task 6.3: Unit tests covering duplicate check-ins and late check-in rejection.

---

### Phase 7: Teacher Web Console Integration & Verification Gate

**Files:**
- Modify: `apps/teacher/api.go`
- Modify: `apps/teacher/main.go`
- Modify: `apps/teacher/web/index.html`
- Modify: `web/index.html`
- Modify: `apps/agent/main.go`
- Create: `apps/teacher/api_phase2_test.go`

**Key Components:**
- REST endpoints for all new capabilities (`/api/v1/broadcast/*`, `/api/v1/scripts/*`, `/api/v1/inventory/*`, `/api/v1/attendance/*`).
- Web console tabs and action modals:
  - 📢 屏幕广播与学生演示控制条
  - 🎨 实时屏幕画板工具栏 (红笔、箭头、荧光笔、清空)
  - 💻 批量脚本下发执行终端 (含实时输出预览)
  - 📋 机房资产与系统监控大屏 (CPU/内存/磁盘图表)
  - 🙋‍♂️ 电子点名签到与导出
- Verification:
  - `go test -race -shuffle=on -count=1 -timeout=5m ./...`
  - `go vet ./...`
  - `gofmt -l .`
  - `git diff --check`
  - Compilation of `bin/agent.exe` and `bin/teacher.exe`
