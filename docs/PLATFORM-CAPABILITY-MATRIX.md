# Platform capability matrix

The matrix separates compile support from runtime capability support. An
unsupported capability is reported explicitly and remains disabled by default.

| Capability | Windows | Linux | macOS | Process boundary |
|---|---|---|---|---|
| TLS / QUIC control | contract + native CI | contract + native CI | contract target | Server/Agent |
| Local IPC | named pipe | Unix socket target | Unix socket target | User Agent/Service |
| Service lifecycle | SCM adapter target | systemd adapter target | launchd adapter target | Service |
| Process control | Service/worker | Service/worker | Service/worker | Worker |
| Screen capture | worker target | worker target | worker target | Worker |
| Input control | explicit capability | explicit capability | explicit capability | Worker |
| Power control | explicit capability | explicit capability | explicit capability | Service |

The first implementation milestone provides the platform ports, common fakes,
capability registry, and worker supervisor. Native desktop capabilities require
platform CI or lab evidence before they are enabled.
