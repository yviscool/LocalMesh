# Multi-platform delivery roadmap

The cross-platform architecture is based on the Veyon research documented in
[`VEYON-RESEARCH.md`](VEYON-RESEARCH.md). LocalMesh keeps policy and protocol
portable while isolating OS APIs behind platform adapters.

## Milestones

1. Platform ports and contract fakes for Windows, Linux, and macOS.
2. Named pipe, Unix socket, and launch/service lifecycle adapters.
3. Capability descriptor registry with platform support and safe defaults.
4. Worker supervisor for high-risk features.
5. Cross-platform build and contract-test matrix.

Each milestone requires a platform-neutral fake, at least one native adapter,
failure/restart tests, and documentation of unsupported capabilities.
