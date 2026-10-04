# Platform policy

LocalMesh is multi-platform by design. Windows, Linux, and macOS are first-class
targets; no product rule assumes Windows as the default operating system.

Platform-specific APIs belong only in `internal/platform/<os>` or `native/`.
Domain, Application, Protocol, Storage, and transport contracts remain portable.
Each platform supplies its own service lifecycle, local IPC, user session, and
privileged capability adapters.

The project intentionally does not preserve backward compatibility for obsolete
interfaces or wire contracts. A breaking change must update all in-repository
callers, migration documentation, tests, and CI in the same change. Unsupported
older clients are rejected explicitly.

## Runtime defaults

- Device platform is discovered at enrollment; persistence defaults to `unknown`.
- Local IPC selects named pipes on Windows and Unix sockets on Linux/macOS.
- Privileged operations run in the platform Service/Daemon.
- Interactive operations run in User Agent/Teacher App.
- High-risk capabilities run in isolated workers where supported.
- Capability descriptors are validated against the current platform at startup.

## Support levels

| Level | Meaning |
| --- | --- |
| Contract | Portable interface and fake/contract test exist |
| CI | Platform build and automated test run in CI |
| Native | Platform adapter is implemented |
| Lab | Real desktop/service behavior verified on hardware |

Feature availability must report its support level. It must never silently
fallback to a Windows-only implementation.
