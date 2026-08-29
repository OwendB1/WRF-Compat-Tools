# WRF Linux investigation reference

This directory records the War Robots: Frontiers Linux investigation through
2026-08-29. It separates observations from hypotheses and excludes credentials
and raw private captures.

## Current result

- Older desktop routes could reach `ACH=118`, anti-cheat `Online`, and a real
  MRAC exchange before a later remote close. That value is historical evidence,
  not the current supported Deck target.
- The current native build 133 route logs `ACH=77`; the installed Steam build
  132 route logs `ACH=87`. Both reach AC Online but stop before a non-empty
  Gate0018/MRAC request and close after approximately 8.3 and 16.1 seconds.
- Current native-auth launcher contracts also reproduce ACH 120 and 128. Both
  inherit the same mode, emit no MRAC request, and close about 8–9 seconds after
  AC Online. ACH 118 remains historical-only on the current installation.
- A same-build retail Deck control now reports `ACH=87`, Valve Proton
  `11.0-100`, and anti-cheat DLL hashes identical to the current desktop Steam
  copy. It begins 4096-byte MRAC exchanges about 4.7 seconds after AC Online and
  remains healthy. ACH 87 therefore does not distinguish the supported Deck
  from the failing desktop.
- Socket tracing shows a remote orderly TCP close. The client reports WebSocket
  code `1006` because no WebSocket close frame arrived.
- The shared failure means MY.GAMES authentication and the launcher rewrite are
  not the root cause. The game polls the local anti-cheat `Gate0018`, receives
  zero request bytes, emits no MRAC request, and then loses the backend lease.
- A 64-bit Wine boundary probe shows all four current ACH routes give the game
  `AC_LAUNCHMODE=0x00009609`. A payload-free decision probe further proves the
  current native 77 and Steam 87 services return HTTP 200 and fully select that
  exact value, rather than falling back after a request failure. ACH is
  therefore a distinct, still-opaque MGL controller field rather than the
  decimal rendering of `AC_LAUNCHMODE`. Channel 47, `SteamDeck=1`, and Proton
  do not directly choose ACH.

`ACH` is an opaque launcher field. Its meanings below are inferred from
controlled behavior, not documented by MY.GAMES or Valve.

## Documents

- [Investigation history](01-investigation-history.md)
- [Route and result matrix](02-route-matrix.md)
- [Compatibility implementation](03-compatibility-implementation.md)
- [Evidence and security rules](04-evidence-and-security.md)
- [SteamOS/QEMU experiment plan](05-steamos-vm-plan.md)
- [Backend validation investigation](06-backend-validation-investigation.md)
- [ACH and `AC_LAUNCHMODE` analysis](07-ach-launch-mode-analysis.md)

Current evidence points specifically at the protected anti-cheat request
generator rather than general backend transport or the ACH number. The next
software A/B is current build `24860441` on Valve Proton `11.0-100`, matching
the healthy Deck, through the signed-in Steam ACH 87 route. A native ACH 77
control on that exact runtime still emitted no MRAC request and closed after
`8.648 s`, so the runtime switch alone is insufficient. RPC IDs are
per-connection correlation numbers, not a fixed MRAC channel.
