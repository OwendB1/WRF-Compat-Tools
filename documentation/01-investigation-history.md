# Investigation history

## Goal and constraints

The original goal was to run the native MY.GAMES installation of War Robots:
Frontiers on Linux, preserve the existing MY.GAMES profile, and use the
anti-cheat route selected for Steam Deck. The standalone launcher selected
`ACH=77`; an older Deck-compatible Steam route selected `ACH=118` and ran the
anti-cheat client in user mode. A current same-build Deck control later proved
that the supported route now selects ACH 87.

All work stayed on the compatibility side: implement missing Windows behavior,
use authentic public host data, observe launcher-generated messages, and test
official binaries. It did not fabricate hardware/security state, invoke private
anti-cheat APIs, alter anti-cheat payloads, or handle user credentials in a new
launcher.

Support later confirmed that this compatibility testing was permitted, but
could not provide the private SDK, headers, or protocol definitions. That led
to clean-room observation of the launcher's own local job contract, not
reconstruction of anti-cheat attestation.

## Test system

- Nobara Linux 44, KDE Plasma on Wayland
- Kernel `7.1.3-200.nobara.fc44.x86_64`
- AMD Ryzen 7 7800X3D
- NVIDIA RTX 4080 SUPER, driver 595.84
- 93 GiB visible RAM
- Secure Boot disabled
- Steam app ID `1491000`
- Controlled Steam build ID `24552611`; game `1.13.46.9422`

## Phase 1: identify the launcher routes

The standalone MY.GAMES installation used:

- `SGL=launchpad_13.2017027`
- `Channel=31`
- `-ClientAuthType=GameCenter`
- launcher-provided MY.GAMES token and user ID

It consistently selected `ACH=77`. Adding environment hints such as
`SteamDeck=1` and `SteamEnv=1`, or forcing channel 47 without the Steam launch
mode, did not change that result.

The Steam installation used:

- `SGL=Steam_13.2017027`
- a Steam-specific protocol and channel 47
- Steam authorization jobs and a Steam ticket
- the `-fromsteam`/Steam launch path

The launcher executable itself was byte-identical between installations. Its
configuration, invocation, authorization jobs, and server-selected policy were
different. Adding `-fromsteam` was therefore not account-neutral: it started
Steam authorization and selected the Steam identity.

## Phase 2: make the standalone ACH 77 route progress

Stock Wine/Proton lacked several Windows interfaces queried by the anti-cheat.
A custom GE-Proton11-3-based runtime added narrowly scoped compatibility:

1. ACPI firmware-table enumeration through
   `NtQuerySystemInformation(SystemFirmwareTableInformation)`, backed by a
   captured real host TPM2 ACPI table.
2. `SystemDmaGuardPolicyInformation` class 202 with the host/runtime-appropriate
   one-byte false response, removing `STATUS_INVALID_INFO_CLASS 0xC0000003`.
3. Exposure of the real host TPM endorsement public key as a validated
   `BCRYPT_RSAKEY_BLOB` through the Platform Crypto Provider `PCP_EKPUB`
   property. No private key was exposed.
4. `NCryptDecrypt` routing through Wine's existing BCrypt implementation.
5. Narrow Schannel diagnostics recording statuses and sizes, never HTTP bodies.

This moved the route from early failures (`0xFF01DEAD` and the class-202
`c0000003` error) through an anti-cheat HTTPS 200 response to `ACClient:
Online`, the MY.GAMES profile, and the hangar. The session still disconnected.
Earlier repetitions showed launcher/backend errors such as `90026 1` and
`90012 e04d0402` at roughly 15-second intervals. An HTTP 200 proved transport,
not server acceptance of the complete session.

## Phase 3: repair the Steam ACH 118 launch path

Runtime and mount layout changed routing:

- GE-Proton10-34 with Steam Linux Runtime 3 (`sniper`) and the normal `Z:` path
  selected `ACH=118`.
- Proton Experimental with Steam Linux Runtime 4 and an `S:` path selected
  `ACH=77` during comparison.

The GCLay overlay initially caused a D3D12 access violation and prevented the
game window. Disabling both overlay DLLs fixed that layer:

```bash
WINEDLLOVERRIDES="GCLay.dll=d;GCLay64.dll=d" SteamDeck=1 %command%
```

The game then froze during anti-cheat initialization. Loader tracing found that
the protected Steam `acclient64.dll` had relocated away from its preferred
image base, while four pointers in its TLS directory still contained
preferred-base addresses. Wine's `alloc_tls_slot()` dereferenced the stale
`AddressOfIndex` while holding the loader lock, causing the apparent hang.

A narrow ntdll patch rebases a TLS directory pointer only when all of these are
true:

- the image was relocated;
- the pointer still falls inside the original preferred-image range; and
- the corrected pointer falls inside the mapped image.

The patched `GE-Proton10-34-WRF-TLS` runtime allowed the Steam route to reach
`ACH=118`, anti-cheat `Online`, account initialization, and the hangar.

## Phase 4: search for an account-neutral route

Several controlled combinations were tried:

- Standalone launcher plus modified policy/configuration: `ACH=128`, then
  disconnect. `128` appeared to be a Windows-oriented route, but that meaning
  remains an inference.
- MY.GAMES authorization with official Steam game binaries: `ACH=120`, then
  disconnect, repeated.
- Complete official Steam route: `ACH=118`, but it authenticated the Steam
  account. The original configuration was restored after the test.
- A search for an in-game Switch Account, Server Authorization, or MY.GAMES
  login path found none.
- A temporary cache/credential pass-through test pointed the official Steam
  launcher at the native launcher cache and restored it afterward. It prompted
  for verification and then selected `ACH=120`, not 118.

These tests showed that game binaries alone do not select the route. Launcher
job metadata, authorization mode, and server policy participate.

## Phase 5: observe and replay the local launcher job

The investigation added a narrow Wine named-pipe capture/rewrite facility and
an analyzer. The launcher emits a `GCJobGameLaunch` XML job containing the game
executable, working directory, command-line parameters, anti-cheat launch
library, `ACH`, platform/application fields, and the already-established
launcher identity.

The first native MY.GAMES capture targeting Steam-like binaries retained
MY.GAMES identity but selected `ACH=120` with no Steam app ID.

A later controlled pipe rewrite changed the launch target contract after MGL
had selected ACH 120:

- official Steam game executable and directory;
- official Steam anti-cheat launch library;
- `SteamAppId=1491000`;
- `ACH=118`;
- Steam launch flags `-nosplash -LaunchFromSteam`.

It preserved the launcher-generated MY.GAMES account fields. The resulting game
reported `BackendPlatform: MyGames`, reached `ACH=118`, anti-cheat `Online`, the
correct MY.GAMES profile, and the hangar. This established the originally sought
combination without custom credential collection or custom anti-cheat calls.

The session disconnected after approximately 61–65 seconds.

## Phase 6: compare the failure with an unmodified Steam control

The original disconnect detector searched for the literal word `disconnect`.
The actual game event was `Connection was closed with: 1006`, so the detector
had missed it.

Three fresh normal-Steam controls on 2026-08-07 all used the current installed
build, reached `ACH=118`, anti-cheat `Online`, and a Steam profile, then failed
at the same approximately 64-second boundary as the MY.GAMES hybrid.

Narrow Winsock tracing established:

- backend: `default.live.sprw.mygames.zone` / `45.66.99.17:5020`;
- the TCP connection remained bidirectional through later client sends and
  server replies;
- after about 63.8 seconds, three receive calls returned success with zero
  bytes;
- the client had not called `shutdown`; it called `closesocket` only after the
  zero-byte receives.

This is a remote orderly TCP close. WebSocket code 1006 appears because the
peer closed TCP without a WebSocket close frame. Some cycles received an
unhandled backend message type 0 immediately before closure, but causality is
not yet proven.

Narrow Schannel tracing established a separate anti-cheat exchange:

- TLS to `wrf.anticheat.my.games` completed;
- the client sent a 194-byte application record and received a 184-byte reply;
- TLS ended normally with `close_notify` and context deletion;
- `ACClient: Online` followed about 0.4 seconds later;
- no later anti-cheat retry or Schannel error occurred near the backend cutoff.

## Phase 7: isolate the 60-second backend boundary

The official client's own verbose RPC logging was enabled for one normal Steam
control. A payload-free analyzer retained only timestamps, direction, RPC ID,
method name, and encoded payload length. Raw verbose logs remain private because
they include authentication material.

Measured from WebSocket establishment:

- platform authorization completed at `0.784 s`;
- anti-cheat reported online at `0.839 s`;
- the initial session ping completed at `1.868 s`;
- another session ping completed at `30.336 s`;
- an analytics event completed at `55.016 s`; and
- the remote connection closed at `60.023 s`.

No RPC was awaiting a response at closure. Most importantly, the client made no
`FMracServiceWs::ClientRequest` call.

A second verbose control reproduced the result with one extra detail. A normal
session ping completed at `60.230 s`. At `61.851 s`, the server sent an empty
message with ID 0 for which the client had no handler; the remote close followed
33 milliseconds later. This message is not invariant: it appeared in only two
of seven retained disconnect summaries. It is therefore a close/rejection
correlate, not evidence of a challenge the client should answer.

A fresh native-auth comparison selected ACH 120. Unlike ACH 118, it sent
`FMracServiceWs::ClientRequest` at `5.456 s` and received a normal RPC response
at `5.539 s`. The backend then closed at `8.600 s`. Its primary auth was the
default `MyGames` type with no additional platform-auth request. This proves the
MRAC backend method works through Wine and separates two failure stages: ACH 120
reaches the MRAC exchange and is rejected soon afterward, while ACH 118 never
emits that exchange before its later cutoff.

Static inspection found the corresponding official-client bridge. After the
anti-cheat online callback, the game registers a request delegate. When invoked,
that delegate obtains an opaque request from the anti-cheat client, sends it via
`FMracServiceWs::ClientRequest`, and returns the opaque server response to the
anti-cheat client. No message contents need to be decoded or altered.

The `MRAC` log category was active, but no request-attempt, response, or failure
message appeared before the ACH 118 disconnect. An older MY.GAMES log did show
repeated client-request failures, starting only after its backend connection had
already closed. That proves the path can be invoked in another route, but is not
yet a clean successful baseline.

## Present conclusion

The hybrid launcher job and MY.GAMES account are no longer the leading cause:
the unmodified Steam-account control fails in the same way. Wine is also not
locally aborting the backend socket. A server or upstream session component
closes it at what appears to be a later validation/heartbeat boundary.

The strongest remaining hypothesis is a missing or late anti-cheat-generated
client request: backend transport and ordinary RPC traffic remain healthy until
an exact 60-second lease or validation boundary, while the built-in MRAC bridge
does not send anything. Cause remains unknown: the anti-cheat client may decline
to emit the callback, emit it too late, or depend on a missing Wine behavior.

The clean next experiment is metadata-only comparison against a successful
supported Steam Deck run, followed by narrow tracing around the callback trigger
and its local timer/IPC dependencies. A SteamOS VM can isolate userspace
differences but cannot emulate Deck hardware identity.
