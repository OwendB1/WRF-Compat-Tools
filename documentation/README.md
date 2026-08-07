# WRF Linux investigation reference

This directory records the War Robots: Frontiers Linux investigation through
2026-08-07. It separates observations from hypotheses and excludes credentials
and raw private captures.

## Current result

- The Steam launcher route can reach `ACH=118`, anti-cheat `Online`, and the
  hangar with the custom `GE-Proton10-34-WRF-TLS` runtime.
- A controlled launcher-job rewrite can also reach `ACH=118` while retaining
  the MY.GAMES account (`BackendPlatform: MyGames`).
- Both the unmodified Steam-account control and the MY.GAMES hybrid are then
  disconnected at the same approximately 64-second boundary.
- Socket tracing shows a remote orderly TCP close. The client reports WebSocket
  code `1006` because no WebSocket close frame arrived.
- The shared failure means MY.GAMES authentication and the launcher rewrite are
  not the root cause. The game polls the local anti-cheat `Gate0018`, receives
  zero request bytes, emits no MRAC request, and then loses the backend lease.
- Both GameCenter environment fields and the launcher pipe are present. Current
  work is inside protected request generation, especially exception, unwind,
  per-thread TLS, or unobserved IPC behavior—not ACH selection.

`ACH` is an opaque launcher field. Its meanings below are inferred from
controlled behavior, not documented by MY.GAMES or Valve.

## Documents

- [Investigation history](01-investigation-history.md)
- [Route and result matrix](02-route-matrix.md)
- [Compatibility implementation](03-compatibility-implementation.md)
- [Evidence and security rules](04-evidence-and-security.md)
- [SteamOS/QEMU experiment plan](05-steamos-vm-plan.md)
- [Backend validation investigation](06-backend-validation-investigation.md)

Current evidence points specifically at the anti-cheat request bridge rather
than general backend transport. A successful supported Steam Deck trace remains
the decisive comparison. A QEMU VM is useful as a SteamOS-userspace A/B, but it
does not become a Steam Deck merely by booting SteamOS with UEFI.
