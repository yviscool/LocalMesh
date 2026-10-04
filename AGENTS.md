# LocalMesh Agent Guidelines

## Project goal

LocalMesh is a local-first classroom control platform for Windows, Linux, and macOS. Discovery, identity, classroom membership, explicit authorization, and batch commands are separate concepts. Discovery never grants control.

## Priority

1. User requirements and security boundaries
2. This file
3. Architecture and domain documents under `docs/`
4. Module READMEs and code comments

## Structure

```text
apps/             executable applications
cmd/              Go entry points
internal/         domain, application, infrastructure, and platform adapters
proto/            protocol definitions and generated-code entry points
native/           native OS adapters
web/              teacher UI
docs/             architecture, protocol, ADRs, and roadmaps
tests/            integration and simulation tests
tools/            development and generation tools
deploy/           packaging and service configuration
.agents/skills/   versioned project skills
```

## Design constraints

- Domain and Application code are platform-neutral and never import OS APIs.
- Windows, Linux, and macOS differences live behind `internal/platform` ports.
- Network discovery is not authentication, membership, authorization, or capability.
- DeviceID, TeacherID, and ClassroomID are stable identities; addresses are observations.
- Control and data planes have separate lifecycles.
- Batch operations use `Target + Command + Policy + Result` with deadline, retry, partial success, cancellation, and audit outcome.
- Privileged work runs in the platform Service/Daemon; interactive work runs in User Agent/Teacher App; Web UI never performs privileged operations.
- Dangerous capabilities require explicit authorization and audit logging.
- High-risk capabilities use cancellable worker processes where supported.

## Breaking-change policy

Backward compatibility is not a project requirement. Obsolete protocol, Port, configuration, and storage interfaces are removed or rejected directly. A breaking change updates all repository callers, migrations, tests, and docs in the same change; no compatibility shim or silent downgrade is added.

## Development rules

- Go code uses `gofmt`, `go vet`, and table-driven tests.
- Public protocol changes update `proto/`, migration notes, and rejection tests.
- Structured logs must never contain tokens, private keys, or screen contents.
- New configuration has safe defaults and startup validation.
- New infrastructure exposes a replaceable Port plus fake or contract tests.
- Platform API changes run in the corresponding platform CI or lab environment.

## Verification gate

```text
go test -race -shuffle=on -count=1 -timeout=5m ./...
go vet ./...
gofmt -l .
git diff --check
```

Before implementation, read `aaa.md`, `docs/ARCHITECTURE.md`, `docs/ARCHITECTURE-MULTIPLATFORM.md`, and the relevant project skill.
