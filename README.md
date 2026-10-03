# LocalMesh

Local-first Windows classroom control platform. LocalMesh manages a group of devices inside an explicit classroom boundary; network discovery alone never grants control.

## Current status

Phase 0 is under construction: stable device identity, classroom membership, discovery observations, pairing/session contracts, and command envelopes.

Read the [documentation map](docs/README.md), [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), [docs/ROADMAP.md](docs/ROADMAP.md), and [docs/PHASE-0-STATUS.md](docs/PHASE-0-STATUS.md) before implementing features.

## Development

Requirements: Go 1.23+.

```text
go test -race -shuffle=on ./...
go vet ./...
make check
```

For proxy-based development in the local environment:

```text
export https_proxy=http://127.0.0.1:7897
export http_proxy=http://127.0.0.1:7897
export all_proxy=socks5://127.0.0.1:7897
```

## Git workflow

- Use short-lived branches: `feat/...`, `fix/...`, `docs/...`, `chore/...`.
- Keep commits atomic and imperative, for example `feat(domain): add pairing lifecycle`.
- Run `make check` before commit and push.
- Pull requests must explain behavior, tests, security impact, and rollout concerns.
- Never commit credentials, machine-specific config, generated binaries, or screen data.
