# Infrastructure Wave Implementation Status

This document records the first completed vertical slices from the next-wave plan.

## Session recovery

`internal/application/sessionrecovery` evaluates durable sessions after restart.
It closes expired sessions, sessions whose classroom membership is missing or
revoked, and older sessions when one device has multiple active records. Closed
records are never reopened. Recovery does not authenticate a device; a fresh
challenge-response exchange is still required.

SQLite implements the recovery query contract in `internal/storage/sqlite` with
timestamp parsing and deterministic newest-first ordering per device.

## UDP discovery

`internal/transport/discovery` accepts a versioned JSON announce packet over UDP.
It enforces a packet-size limit, validates protocol version and stable DeviceID,
suppresses duplicate announcements, and exits on context cancellation or socket
closure. Accepted packets become short-lived discovery observations only; they
do not create classroom membership or capabilities.

## Verification

Focused tests cover recovery decisions and UDP announce acceptance, rejection,
duplicate suppression, and shutdown. The repository gate remains:

```text
go test -race -shuffle=on ./...
go vet ./...
gofmt -l .
git diff --check
```

## IPC and network lab foundations

`internal/transport/ipc` now defines a bounded length-prefixed JSON contract
for Service/User Agent adapters. It handles partial reads, oversized frames,
request IDs, duplicate requests, capability fields, and explicit responses.
The framing layer is transport-neutral so the Windows named-pipe endpoint can
be added without changing the contract.
Every request can be re-authorized against its session, classroom, capability,
and target scope before the privileged handler runs.

The endpoint boundary is represented by `Listener` and `ServeListener`.
Non-Windows builds return an explicit unsupported error; Windows builds use a
named-pipe listener while reusing the same framing and authorization layers.

`internal/simulation/network` provides deterministic latency, loss and
duplication injection with seeded randomness and payload isolation. It is the
base for turning the compatibility matrix into repeatable integration tests.
Its classroom router records isolation violations so cross-classroom delivery
can be asserted as a zero-tolerance invariant.

## Test hang diagnosis

The first race run after a clean Go build cache can spend several minutes
compiling `modernc.org/sqlite`; Go emits no test output during compilation.
This was measured and reproduced, then confirmed to finish with all packages
passing. All Make targets now use a five-minute test timeout, and
`tools/test-progress.ps1` prints explicit start/end markers so a real deadlock
is distinguishable from cold dependency compilation.
