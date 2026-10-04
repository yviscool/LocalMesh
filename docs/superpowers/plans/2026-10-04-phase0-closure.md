# Phase 0 Closure Implementation Plan

> **For agentic workers:** Execute this plan task-by-task with a test-first loop. Each task must leave the repository buildable and independently revertible.

**Goal:** Close the remaining Phase 0 gaps so LocalMesh has a verified control transport, a real Windows Service boundary, and repeatable network compatibility evidence.

**Architecture:** Keep Domain and Application transport-neutral. QUIC and named pipes are adapters behind existing ports; authentication and classroom authorization run before command execution. The network lab remains deterministic and in-process, with optional OS-level tests gated by platform.

**Tech Stack:** Go 1.23+, existing `protocol.Envelope`, `CommandTransport`, TLS configuration, SQLite, `quic-go` only if dependency review accepts it, Windows named pipes through `golang.org/x/sys/windows`, GitHub Actions.

**Spec:** `docs/FOUNDATION.md`, `docs/PROTOCOL.md`, `docs/NETWORK-MATRIX.md`, `docs/ROADMAP.md`, `AGENTS.md`.

## Global Constraints

- Discovery never grants membership, capability, or control.
- DeviceID is stable; IP addresses are mutable observations.
- Every control request requires authenticated session, classroom scope, capability, deadline, and audit outcome.
- Windows privileged operations stay in Service code; User Agent carries no Service private key.
- New dependencies require a `go.mod` diff, license/source review, and a focused integration test.
- Every implementation slice must pass `gofmt`, focused race tests, `go vet`, and `git diff --check`.

## Review Focus

- A reconnect must not replay a command with the same idempotency key.
- A stale or revoked session must fail before a QUIC stream reaches a command handler.
- A named pipe client must not gain privilege by choosing a different capability string.
- A cancelled listener must close all accepted connections and leave no goroutines.
- A simulated network must report zero cross-classroom deliveries under loss, duplication, and reordering.

### Task 1: Harden IPC endpoint lifecycle

**Files:**
- Modify: `internal/transport/ipc/endpoint.go`
- Modify: `internal/transport/ipc/pipe_windows.go`
- Create: `internal/transport/ipc/endpoint_test.go`

**Interfaces:** `Listener.Accept(context.Context) (io.ReadWriteCloser, error)` and `ServeListener` remain the only upper-layer contract.

- [ ] Add failing tests for cancellation while accepting, listener close idempotency, and a connection error followed by accepting the next client.
- [ ] Implement a close state guarded by a mutex and ensure `Accept` returns `context.Canceled` or `io.EOF` deterministically.
- [ ] On Windows, close the pipe handle when context cancellation interrupts `ConnectNamedPipe`; avoid a goroutine that survives listener shutdown.
- [ ] Run `go test -race ./internal/transport/ipc` and a Windows cross-compile check.
- [ ] Commit `fix: make ipc listener shutdown deterministic`.

### Task 2: Add application-level IPC command adapter

**Files:**
- Create: `internal/application/ipc/service.go`
- Create: `internal/application/ipc/service_test.go`
- Modify: `internal/transport/ipc/framing.go`

**Interfaces:** `Service.Handle(context.Context, ipc.Request) ipc.Response` converts a validated request to the existing command application service.

- [ ] Write failing tests for unauthorized capability, classroom mismatch, malformed target, deadline exceeded, and successful command submission.
- [ ] Implement conversion without duplicating authorization rules; call the existing authorization/application ports.
- [ ] Ensure response codes are stable and never include private keys, tokens, or screen content.
- [ ] Run focused race tests and verify audit events are emitted once for duplicate requests.
- [ ] Commit `feat: connect ipc requests to command application service`.

### Task 3: Introduce QUIC transport contract

**Files:**
- Create: `internal/transport/quic/client.go`
- Create: `internal/transport/quic/client_test.go`
- Modify: `go.mod`, `go.sum` only after dependency review

**Interfaces:** `Client.Send(context.Context, protocol.Envelope) error` implements `application.CommandTransport`.

- [ ] First add a fake stream test proving envelope size, deadline, and idempotency metadata requirements.
- [ ] Select and pin a maintained QUIC library; record the decision in `docs/DECISIONS.md`.
- [ ] Implement one control stream per command with bounded concurrent streams and connection close on protocol violation.
- [ ] Bind the authenticated peer identity to DeviceID before sending an envelope.
- [ ] Add tests for timeout, reconnect, duplicate idempotency key, unsupported protocol version, and certificate/DeviceID mismatch.
- [ ] Commit `feat: add quic control transport` only after dependency and license checks pass.

### Task 4: Expand deterministic network compatibility lab

**Files:**
- Modify: `internal/simulation/network/faults.go`
- Create: `internal/simulation/network/scenarios_test.go`
- Modify: `docs/NETWORK-MATRIX.md`

**Interfaces:** Add deterministic latency distribution, reordering, reconnect events, and per-classroom delivery counters without changing production transports.

- [ ] Add failing scenarios for 1%, 5%, and 15% loss; 10ms, 100ms, and 500ms latency; duplication; reordering; DHCP address changes; and two classrooms on one subnet.
- [ ] Implement seeded event scheduling and bounded event history so 1,000 agents cannot cause unbounded memory growth.
- [ ] Assert discovery success, reconnect completion, duplicate command rate, P95 latency, and isolation violations.
- [ ] Run the scenario suite under `-race` and document thresholds and known non-goals.
- [ ] Commit `test: add phase zero network compatibility scenarios`.

### Task 5: CI and release gates

**Files:**
- Modify: `.github/workflows/test.yml`
- Modify: `Makefile`
- Create: `.github/workflows/windows.yml`
- Modify: `docs/OPERATIONS.md`

- [ ] Add Windows CI for native IPC compile and named-pipe contract tests.
- [ ] Keep Linux CI for race, vet, formatting, vulnerability scan, and 100-agent simulation.
- [ ] Add explicit test timeouts and artifact upload for failed test logs.
- [ ] Document local commands, cold-cache expectations, and how to distinguish compilation delay from a deadlock.
- [ ] Require all gates to pass before marking Phase 0 complete.
- [ ] Commit `ci: add cross-platform phase zero quality gates`.

## Completion Gate

Run after all tasks:

```text
gofmt -w internal
go test -race -shuffle=on -count=1 -timeout=5m ./...
go vet ./...
gofmt -l .
git diff --check
git status --short --branch
```

Phase 0 is complete only when Linux and Windows CI are green, the QUIC and IPC integration tests provide pass/fail evidence, and the network lab reports zero classroom isolation violations.
