# LocalMesh

Local-first Windows classroom control platform. LocalMesh manages a group of devices inside an explicit classroom boundary; network discovery alone never grants control.

## Current status

Phase 0 foundation is executable: stable device identity, classroom membership, discovery/session contracts, protocol validation, idempotency, safe defaults, and 100-agent simulation. SQLite and real network adapters are the next implementation slice.

Read the [documentation map](docs/README.md), [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), [docs/ROADMAP.md](docs/ROADMAP.md), and [docs/PHASE-0-STATUS.md](docs/PHASE-0-STATUS.md) before implementing features.

## Development

Requirements: Go 1.23+.

```text
go test -race -shuffle=on ./...
go vet ./...
make check
```

网络访问按当前环境选择可用的国内镜像或受控的外部访问方式；相关配置只保存在本机环境，不写入仓库。

依赖下载、GitHub 操作和 CI 使用仓库配置的标准入口；遇到网络问题时优先检查镜像、凭据和组织网络策略。

## Git workflow

- Use short-lived branches: `feat/...`, `fix/...`, `docs/...`, `chore/...`.
- Keep commits atomic and imperative, for example `feat(domain): add pairing lifecycle`.
- Run `make check` before commit and push.
- Pull requests must explain behavior, tests, security impact, and rollout concerns.
- Never commit credentials, machine-specific config, generated binaries, or screen data.
