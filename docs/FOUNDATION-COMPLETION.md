# Foundation completion gate

The portable foundation is now composed of:

- Domain/Application/Protocol boundaries with no OS imports.
- `platform.Runtime` and platform-specific endpoint ports.
- Unix socket endpoint contract for Linux/macOS.
- Windows named-pipe adapter boundary with explicit native verification gate.
- Safe default capability registry with platform validation.
- Cancellable worker supervisor with running/stopped/crashed state.
- Health registry for startup and runtime diagnostics.
- SQLite migrations with no implicit Windows platform default.

Business capability work may start against these contracts. A release is not
production-ready until Windows named-pipe ACL/identity tests, native service
lifecycle tests, and desktop lab tests pass on their target platforms.
