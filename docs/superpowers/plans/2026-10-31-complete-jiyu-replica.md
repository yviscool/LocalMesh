# LocalMesh: Full JiYu (极域) Classroom Suite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a complete, production-grade replication of JiYu (极域电子教室) classroom management features on LocalMesh, combining Magpie's native Wails/WebView2/Win32 client architecture with Veyon's isolated worker/capability plugin model.

**Architecture:** 
1. **Teacher Console & Student Agent** built on Go + Wails/WebView2 (inspired by Magpie's native client pattern: lightweight, tray-driven, single-instance mutex, registry autostart, win32 API wrappers).
2. **Platform Capabilities** running through LocalMesh's Hexagonal Architecture: Domain -> Application -> Capability Registry -> Worker Supervisor (inspired by Veyon's worker separation model: screen capture, input control, and anti-tampering run in isolated, cancellable worker processes).
3. **Data & Control Planes** separated: Reliable commands, policies, and file transfers over QUIC/TLS/IPC; ultra-low latency desktop streaming and thumbnails over dedicated data channels.

**Tech Stack:** Go 1.23, Wails v3 / Windows WebView2, Pure Win32 Syscalls (via `golang.org/x/sys/windows`), QUIC (`quic-go`), DXGI Desktop Duplication / Win32 GDI, SQLite (`modernc.org/sqlite`), Protobuf / JSON Envelopes.

**Specs & Reference:** 
- LocalMesh Architecture: [`docs/ARCHITECTURE.md`](file:///C:/Users/Administrator/Desktop/LocalMesh/docs/ARCHITECTURE.md), [`docs/ARCHITECTURE-MULTIPLATFORM.md`](file:///C:/Users/Administrator/Desktop/LocalMesh/docs/ARCHITECTURE-MULTIPLATFORM.md)
- Veyon Research: [`docs/VEYON-RESEARCH.md`](file:///C:/Users/Administrator/Desktop/LocalMesh/docs/VEYON-RESEARCH.md), `C:\Users\Administrator\Desktop\veyon-research-20261004115645`
- Magpie Native Reference: `C:\Users\Administrator\Desktop\Forge\upstream\magpie`

## Global Constraints

- Domain and Application remain 100% platform-neutral; no direct OS APIs or `runtime.GOOS` branches in domain.
- All OS-specific code resides in `internal/platform/` or dedicated worker packages.
- Discovery never grants control; authentication uses Ed25519 challenge-response; authorization uses Capability checks.
- Privileged operations reside in Service/Daemon; interactive UI in User Agent / Teacher Console.
- High-risk capabilities (screen capture, input control, anti-tampering lock) run in cancellable worker processes.
- Verification gate: `go test -race -shuffle=on -count=1 -timeout=5m ./...`, `go vet ./...`, `gofmt -l .`, `git diff --check`.

## Review Focus

1. **AP Isolation & Multi-Classroom Separation**: Two teachers on the same Wi-Fi subnet cannot control each other's students.
2. **Anti-Tampering Resilience**: Student Agent cannot be terminated via Windows Task Manager, taskkill, or non-elevated user actions.
3. **Input & Lock Screen Trapping**: Screen lock effectively traps mouse/keyboard, suppressing Alt+Tab, Win keys, and desktop gestures without leaking keyboard state.
4. **Network Drop & Recovery**: Student IP changes via DHCP or Wi-Fi reconnect seamlessly recover session without orphaned command states.
5. **Zero Deadlocks / Leaks in Streaming**: Desktop capture worker recovers cleanly if teacher disconnects or network drops.

---

## Phase 1: Native Client & Shell Foundation (参考 Magpie)

### Task 1.1: Windows Native Endpoint & Named Pipe ACL
**Files:**
- Create: `internal/platform/windows/namedpipe.go`
- Create: `internal/platform/windows/namedpipe_test.go`
- Modify: `internal/platform/common/endpoint_windows.go`

**Interfaces:**
- Consumes: `internal/platform.LocalEndpoint`
- Produces: Concrete named pipe listener with proper Windows Security Descriptor (SDDL restricting access to SYSTEM, Administrators, and active interactive user).

- [ ] Step 1: Write test verifying Windows named pipe creation, connection, and permission rejection.
- [ ] Step 2: Implement named pipe server with Win32 `CreateNamedPipeW` and security attributes.
- [ ] Step 3: Run tests and verify PASS.

### Task 1.2: Native Shell Lifecycle (Tray, Single-Instance, Autostart)
**Files:**
- Create: `internal/platform/windows/lifecycle.go`
- Create: `internal/platform/windows/lifecycle_test.go`
- Reference: `C:\Users\Administrator\Desktop\Forge\upstream\magpie\internal\autostart`, `internal\gui\instance_windows.go`

- [ ] Step 1: Write tests for single-instance mutex lock and registry Run key autostart toggle.
- [ ] Step 2: Implement Windows `CreateMutexW` single-instance check and Registry autostart helper.
- [ ] Step 3: Run tests and verify PASS.

---

## Phase 2: Core Classroom Control Capabilities (参考 Veyon / 极域)

### Task 2.1: Lock Screen & Input Suppression Capability
**Files:**
- Create: `internal/capabilities/screenlock/lock.go`
- Create: `internal/capabilities/screenlock/lock_test.go`
- Create: `internal/capabilities/screenlock/worker/main.go`
- Reference: Veyon `plugins/screenlock`

- [ ] Step 1: Define `screenlock.lock` and `screenlock.unlock` capability descriptors and payloads (custom banner text, title, opacity).
- [ ] Step 2: Implement lock worker process: creates topmost borderless window, installs low-level keyboard/mouse hooks (`WH_KEYBOARD_LL`, `WH_MOUSE_LL`) to suppress Win/Alt+Tab/Ctrl+Esc.
- [ ] Step 3: Connect lock capability to Worker Supervisor.
- [ ] Step 4: Run tests and verify PASS.

### Task 2.2: Power Management Capability (WOL, Shutdown, Reboot, Logoff)
**Files:**
- Create: `internal/capabilities/power/power.go`
- Create: `internal/capabilities/power/power_test.go`
- Create: `internal/capabilities/power/wol.go`
- Reference: Veyon `plugins/powercontrol`

- [ ] Step 1: Implement Wake-on-LAN magic packet broadcaster (`wol.go`).
- [ ] Step 2: Implement Windows `InitiateSystemShutdownExW` and `ExitWindowsEx` wrappers with audit trails.
- [ ] Step 3: Write tests for power capability execution, validation, and audit events.
- [ ] Step 4: Run tests and verify PASS.

### Task 2.3: Process & Peripheral Restrictions (Blacklist/Whitelist, USB Block)
**Files:**
- Create: `internal/capabilities/restrictions/process.go`
- Create: `internal/capabilities/restrictions/process_test.go`
- Create: `internal/capabilities/restrictions/usb.go`
- Reference: 极域进程与U盘封锁机制

- [ ] Step 1: Implement process supervisor scanning running processes against blacklist (e.g. taskmgr, games, browsers) and terminating unapproved processes.
- [ ] Step 2: Implement USB storage restriction via Windows Registry policies (`SYSTEM\CurrentControlSet\Services\USBSTOR`).
- [ ] Step 3: Write tests for policy evaluation and event reporting.
- [ ] Step 4: Run tests and verify PASS.

### Task 2.4: File Distribution & Student Homework Collection
**Files:**
- Create: `internal/capabilities/filetransfer/distribute.go`
- Create: `internal/capabilities/filetransfer/collect.go`
- Create: `internal/capabilities/filetransfer/filetransfer_test.go`
- Reference: Veyon `plugins/filetransfer`

- [ ] Step 1: Implement chunked file distribution over QUIC/stream with SHA-256 validation, path traversal prevention, and target directory expansion (Desktop, Documents, Custom).
- [ ] Step 2: Implement student submission collection sink with automatic naming (`StudentName_DeviceID_FileName`).
- [ ] Step 3: Write tests verifying distribution, integrity checks, and collection.
- [ ] Step 4: Run tests and verify PASS.

---

## Phase 3: Desktop Streaming & Remote Control Pipeline (DXGI / Veyon Demo)

### Task 3.1: Screen Capture & Thumbnail Worker
**Files:**
- Create: `internal/capabilities/screen/capture.go`
- Create: `internal/capabilities/screen/capture_test.go`
- Create: `internal/capabilities/screen/worker/capture_windows.go`

- [ ] Step 1: Define screen stream and thumbnail capability protocol.
- [ ] Step 2: Implement desktop capture adapter using DXGI Desktop Duplication API (with GDI fallback for compatibility).
- [ ] Step 3: Implement periodic thumbnail generator for Teacher grid monitoring.
- [ ] Step 4: Run tests and verify PASS.

### Task 3.2: Screen Broadcast (Teacher -> Students) & Remote Input Takeover
**Files:**
- Create: `internal/capabilities/screen/broadcast.go`
- Create: `internal/capabilities/screen/input_inject.go`
- Create: `internal/capabilities/screen/broadcast_test.go`
- Reference: Veyon `plugins/demo`, `plugins/remoteaccess`

- [ ] Step 1: Implement screen broadcast stream transport (one-to-many QUIC streams / multicast data plane).
- [ ] Step 2: Implement student full-screen overlay receiver (Wails/Win32).
- [ ] Step 3: Implement teacher input injection worker (`SendInput` for mouse movement, clicks, keystrokes).
- [ ] Step 4: Write tests and verify PASS.

---

## Phase 4: Classroom Management Suite & Teacher/Student Applications

### Task 4.1: Classroom Interaction (Chat, Raise Hand, Seating Chart)
**Files:**
- Create: `internal/capabilities/interaction/chat.go`
- Create: `internal/capabilities/interaction/raisehand.go`
- Create: `internal/capabilities/interaction/seating.go`
- Create: `internal/capabilities/interaction/interaction_test.go`

- [ ] Step 1: Implement teacher-student instant messaging with broadcast & whisper.
- [ ] Step 2: Implement raise-hand state machine and notifications.
- [ ] Step 3: Implement seating chart model (layout coordinates, seat assignment, student name binding).
- [ ] Step 4: Write tests and verify PASS.

### Task 4.2: Student Agent Executable (`apps/agent`)
**Files:**
- Create: `apps/agent/main.go`
- Create: `apps/agent/config.go`
- Create: `apps/agent/agent_test.go`

- [ ] Step 1: Wire Service lifecycle, Named Pipe, Device identity, UDP Discovery, and Worker Supervisor into `apps/agent`.
- [ ] Step 2: Add self-protection / watchdog loop to keep agent alive.
- [ ] Step 3: Verify build and test.

### Task 4.3: Teacher Console Application (`apps/teacher`)
**Files:**
- Create: `apps/teacher/main.go`
- Create: `apps/teacher/server.go`
- Create: `web/src/...` (Modern React/TypeScript Classroom UI)

- [ ] Step 1: Wire Classroom Engine, SQLite database, QUIC server, Discovery collector, and Control API.
- [ ] Step 2: Integrate Wails/WebView2 desktop shell with tray menu and embedded web UI.
- [ ] Step 3: Verify end-to-end simulation.

---

## Phase 5: Verification Gate & Simulation

- [ ] Run full test suite: `go test -race -shuffle=on -count=1 -timeout=5m ./...`
- [ ] Run `go vet ./...` and `gofmt -l .`
- [ ] Run 100-agent multi-classroom simulation with network faults
