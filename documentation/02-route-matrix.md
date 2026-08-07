# Route and result matrix

`ACH` labels are opaque values observed in launcher jobs. Descriptions are
hypotheses derived from behavior and must not be presented as vendor facts.

| Launcher/auth | Game target | Runtime/context | ACH | Account in game | Result |
| --- | --- | --- | ---: | --- | --- |
| Native MY.GAMES | MY.GAMES | stock/early custom | 77 | MY.GAMES | Early AC failure, then AC Online after Wine compatibility work; disconnect |
| Native MY.GAMES with Steam-like environment | MY.GAMES | `SteamDeck=1`, `SteamEnv=1` | 77 | MY.GAMES | Environment variables alone do not select the route |
| Native MY.GAMES, channel 47 without Steam launch mode | MY.GAMES | custom Proton | 77 | MY.GAMES | Channel alone does not select the route |
| Modified standalone policy | MY.GAMES | custom Proton | 128 | MY.GAMES | Launched after window fix; disconnect |
| MY.GAMES auth | official Steam binaries | custom Proton | 120 | MY.GAMES | MRAC call/response succeeds; remote close at 8.6 s in verbose control |
| Steam launcher/cache pointed temporarily at MY.GAMES cache | Steam | custom Proton | 120 | MY.GAMES verification flow | Did not reach 118 |
| Official Steam route | official Steam binaries | GE-Proton10-34 + SLR3 + `SteamDeck=1` | 118 | Steam | AC Online and hangar; remote close near 64 s |
| Official Steam route | official Steam binaries | Proton Experimental + SLR4/S: comparison | 77 | Steam | Different route selected |
| Launcher-job hybrid | official Steam binaries | MY.GAMES launcher identity + Steam launch contract | 118 | MY.GAMES | AC Online and correct hangar; remote close near 64 s |

## What the matrix proves

- `SteamDeck=1` is not sufficient by itself.
- Steam binaries are not sufficient by themselves.
- Full Steam launch metadata can select 118, but normally selects Steam auth.
- The launcher job can retain MY.GAMES identity while selecting 118.
- Since normal Steam and the hybrid fail at the same later boundary, the hybrid
  rewrite is not the differentiating failure.
- ACH 120 reaches an MRAC call/response before its earlier close; ACH 118 does
  not. General Wine WebSocket/RPC support is therefore not blocking that method.
- ACH 118 remains the target. It reaches AC Online, but the game's `Gate0018`
  poll returns zero request bytes, so current work is below ACH routing.

## Working meaning of observed values

- `77`: standalone/default route on this host.
- `118`: route associated with the Steam Deck/user-mode anti-cheat path.
- `120`: mixed Steam-target/MY.GAMES-auth route.
- `128`: likely Windows-oriented route.

Only the first-order associations are observed. The numeric values are not a
public protocol contract, may change between builds, and must not be hard-coded
as if they were stable API definitions.
