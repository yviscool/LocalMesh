# Foundation review

The previous completion claim was too strong. The portable contracts are in
place, but production readiness still has blocking native work.

## Findings

1. **QUIC is partially closed.** A server accept loop, envelope validation,
   peer DeviceID binding, and handler dispatch now exist. Client acknowledgement,
   certificate-to-DeviceID binding, and reconnect/idempotency integration remain
   required before production release.
2. **Windows IPC is an adapter boundary, not verified native security.** Named
   pipe ACLs and peer identity binding require a Windows runner test; the code
   must not be treated as secure solely because it compiles under a build tag.
3. **Capability startup validation was previously disconnected.** The new
   bootstrap composition now validates platform Runtime and Registry together.
4. **Unix socket permissions were previously unspecified.** Unix endpoints now
   set the socket path to mode `0600` and fail closed if that cannot be applied.

## Verdict

The project is ready to start platform-neutral business use cases and contract
tests. It is **not** yet a production-complete foundation until QUIC server
semantics, Windows ACL/identity tests, native service lifecycle, and lab-only
desktop capability tests are complete.
