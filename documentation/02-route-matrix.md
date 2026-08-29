# Route and result matrix

`ACH` labels are opaque values observed in launcher jobs. Descriptions are
hypotheses derived from behavior and must not be presented as vendor facts.

| Launcher/auth | Game target | Runtime/context | ACH | Game `AC_LAUNCHMODE` | Result |
| --- | --- | --- | ---: | --- | --- |
| Native MY.GAMES | MY.GAMES build 133 | GE-Proton11-6 preferred-base, MGL job mode 2 | 77 | `0x00009609`, measured | AC Online; no MRAC/Gate0018 request; close about 8.2 s later |
| Native MY.GAMES | MY.GAMES build 133 | Valve Proton `11.0-100` | 77 | `0x00009609`, service decision measured | AC Online; no MRAC/Gate0018 request; close 8.648 s later |
| Official Steam route | Steam build 132 | GE-Proton11-6 preferred-base, MGL job mode 5 | 87 | `0x00009609`, measured | AC Online; no MRAC/Gate0018 request; close about 16.1 s later |
| Supported retail Steam Deck | Steam build `24860441` | SteamOS 3.8.16, Valve Proton `11.0-100` | 87 | Unknown | AC Online; recurring 4096-byte MRAC calls receive 697/521-byte responses; session remains healthy |
| Native MY.GAMES auth plus Steam launch contract | MGL-normalized native build 133 | GE-Proton11-6 ACH probe; channel 47 normalized to 31; job mode 3 | 120 | `0x00009609`, measured | AC Online and profile; no MRAC request; close 8.725 s later |
| Native MY.GAMES auth plus legacy Steam-bootstrap contract | MGL-updated legacy copy, build 133 | GE-Proton11-6 ACH probe; job mode 3 | 128 | `0x00009609`, measured | AC Online; no MRAC request; close 8.090 s later |
| Native MY.GAMES | MY.GAMES, older builds | stock/early custom | 77 | Unknown | Early AC failure, then AC Online after Wine compatibility work; disconnect |
| Native MY.GAMES with Steam-like environment | MY.GAMES | `SteamDeck=1`, `SteamEnv=1` | 77 | Unknown | Environment variables alone do not select the route |
| Native MY.GAMES, channel 47 without Steam launch mode | MY.GAMES | custom Proton | 77 | Unknown | Channel alone does not select the route |
| Modified standalone policy | MY.GAMES, older build | custom Proton | 128 | Unknown | Launched after window fix; disconnect |
| MY.GAMES auth | official Steam binaries, older build | custom Proton | 120 | Unknown | MRAC call/response succeeds; remote close at 8.6 s in verbose control |
| Steam launcher/cache pointed temporarily at MY.GAMES cache | Steam, older build | custom Proton | 120 | Unknown | Did not reach 118 |
| Official Steam route | official Steam binaries, older build | GE-Proton10-34 + SLR3 + `SteamDeck=1` | 118 | Unknown | AC Online and hangar; remote close near 64 s |
| Official Steam route | official Steam binaries | Proton Experimental + SLR4/S: comparison | 77 | Unknown | Different route selected |
| Launcher-job hybrid | official Steam binaries, older build | Laboratory pipe rewrite: MY.GAMES identity plus substituted Steam contract and ACH 118 | 118 | Unknown | AC Online and correct hangar; remote close near 64 s |

## What the matrix proves

- `SteamDeck=1` is not sufficient by itself.
- Steam binaries are not sufficient by themselves.
- ACH and `AC_LAUNCHMODE` are separate: current ACH 77, 87, 120, and 128
  launches all deliver `0x00009609` to the game. For 77 and 87, a payload-free
  decision probe proves the service returns HTTP 200 and a fully valid 10-byte
  hexadecimal value that selects `0x00009609`; it is not a request fallback.
- Both the current desktop Steam route and the same-build supported Deck select
  87. ACH is therefore not the supported/failing discriminator.
- Current Steam 87 retains app id 1491000, channel 47, and the same
  `-nosplash -LaunchFromSteam` flags as historical official Steam 118. Those
  local fields are not sufficient selectors.
- The laboratory launcher-job rewrite retained MY.GAMES identity while
  explicitly substituting ACH 118; it did not discover MGL's genuine selector.
- Since normal Steam and the hybrid fail at the same later boundary, the hybrid
  rewrite is not the differentiating failure.
- ACH 120 and the preferred-base ACH 118 route both reached an MRAC
  call/response. General Wine WebSocket/RPC support is therefore not blocking
  that method. Earlier relocated ACH 118 runs that returned an empty Gate0018
  request are a distinct compatibility stage.
- ACH 118 remains useful historical evidence, but it is no longer the current
  comparison target. The current positive control is the same-build Deck on
  ACH 87 and Valve Proton `11.0-100`.
- Current 77, 87, 120, and 128 all stop before a non-empty Gate0018/MRAC
  request despite inheriting the same `AC_LAUNCHMODE`; the next comparison
  point is the request generator boundary, not another blind launch-mode
  override.
- MGL job-mode IDs do not directly encode ACH: game job mode 3 accompanied 77,
  120, and 128 under different current launcher contracts.

## Observed ACH associations

- `77`: current native MY.GAMES association; also seen on older
  standalone and one Proton Experimental comparison.
- `87`: current official Steam association on both the failing desktop and the
  healthy retail Deck.
- `118`: historical desktop Steam-compatible and hybrid assignment; not the
  ACH observed on the current retail Deck.
- `120`: current and historical mixed Steam-contract/MY.GAMES-auth association.
- `128`: current legacy Steam-bootstrap/MY.GAMES-auth association; previously
  described too loosely as a modified standalone policy.

Only the first-order associations are observed. The numeric values are not a
public protocol contract, may change between builds, and must not be hard-coded
as if they were stable API definitions. Their decimal-to-hexadecimal
conversions are not measured `AC_LAUNCHMODE` mappings. Windows API stubs seen
in 77/87 runs are not sufficient evidence that either number means "Windows
attestation."
