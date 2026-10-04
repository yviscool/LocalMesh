# Multi-platform architecture baseline

This document supersedes Windows-only assumptions in earlier architecture
notes. LocalMesh targets Windows, Linux, and macOS with one Domain/Application
contract and platform-specific adapters.

```text
Teacher App / Web UI
        │ authenticated IPC
User Agent (interactive session)
        │ capability + session recheck
Service / Daemon (machine scope)
        │ platform ports
Windows SCM + named pipe | systemd + Unix socket | launchd + Unix socket
```

## Non-negotiable boundaries

- Domain and Application do not import OS APIs or branch on GOOS.
- `internal/platform` is the only platform composition boundary.
- Capability descriptors are checked against the current OS during startup.
- Dangerous capabilities run in cancellable worker processes where available.
- Discovery, TLS, authentication, session authorization, and capability checks
  remain separate regardless of platform.
- Protocol and configuration changes are breaking by default; old versions are
  rejected and no compatibility shim is maintained.

## Delivery levels

Contract tests run on every platform. Native adapters require their platform CI.
Desktop capture, input takeover, and power operations require a lab test before
being enabled in a release profile.
