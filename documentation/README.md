# WRF Linux investigation reference

This directory records the War Robots: Frontiers Linux investigation through
2026-08-29. It separates observations from hypotheses and excludes credentials
and raw private captures.

## Current result

- Before build 133, the Steam launcher route and a controlled MY.GAMES hybrid
  could reach `ACH=118`, anti-cheat `Online`, a real RPC id 47 MRAC exchange,
  and the hangar before the same approximately 64-second remote close.
- The current native build 133 route logs `ACH=77`; the installed Steam build
  132 route logs `ACH=87`. Both reach AC Online but stop before a non-empty
  Gate0018/MRAC request and close after approximately 8.3 and 16.1 seconds.
- Current native-auth launcher contracts also reproduce ACH 120 and 128. Both
  inherit the same mode, emit no MRAC request, and close about 8–9 seconds after
  AC Online. ACH 118 remains historical-only on the current installation.
- Socket tracing shows a remote orderly TCP close. The client reports WebSocket
  code `1006` because no WebSocket close frame arrived.
- The shared failure means MY.GAMES authentication and the launcher rewrite are
  not the root cause. The game polls the local anti-cheat `Gate0018`, receives
  zero request bytes, emits no MRAC request, and then loses the backend lease.
- A 64-bit Wine boundary probe shows all four current ACH routes give the game
  `AC_LAUNCHMODE=0x00009609`, the configured default. ACH is therefore a
  distinct, still-opaque MGL controller field rather than the decimal rendering
  of `AC_LAUNCHMODE`. Channel 47, `SteamDeck=1`, and Proton do not directly
  choose ACH.

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

Current evidence points specifically at the anti-cheat request bridge rather
than general backend transport. ACH 77, 87, 120, and 128 are now current
MGL-selected controls; 118 cannot be recreated faithfully by locally
forcing `AC_LAUNCHMODE`. An official launcher job, developer cohort, or current
supported Steam Deck route must select it with matching policy.
