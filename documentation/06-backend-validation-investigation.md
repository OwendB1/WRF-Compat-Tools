# Backend validation investigation

## Question

What signal is absent before the repeatable backend disconnect, and can it be
identified without decrypting, replaying, or modifying anti-cheat traffic?

## Method

The backend uses TLS-protected WebSockets, so a normal packet capture exposes
transport timing but not application messages. The official game already has
verbose RPC logging. One normal Steam-account ACH 118 control enabled the
relevant log categories and reduced the result to this safe metadata:

- timestamp relative to WebSocket establishment;
- call, response, stream, state, or close direction;
- RPC ID and service/method name; and
- encoded payload length, never payload contents.

Raw verbose logs are private and mode `0600` because RPC bodies contain login
material. No TLS keys, credentials, or opaque anti-cheat bodies were captured or
replayed.

## Observed timeline

| Relative time | Event |
| ---: | --- |
| `0.000 s` | Backend WebSocket established |
| `0.048 s` | `FAuthenticationServiceWs::PlatformAuth` called |
| `0.784 s` | Platform authorization response received |
| `0.839 s` | `ACClient: Online` |
| `0.840 s` | Initial `FSessionProviderServiceWs::Ping` called |
| `1.868 s` | Initial ping response received |
| `30.253 s` | Second session ping called |
| `30.336 s` | Second ping response received |
| `54.914 s` | `FGameAnalyticsServiceWs::LogEvent` called |
| `55.016 s` | Analytics response received |
| `60.023 s` | Remote connection closed; client reports WebSocket `1006` |

No RPC call was awaiting a response at closure. Ordinary application traffic
therefore remained bidirectional five seconds before the cutoff.

A second verbose control tightened this result:

| Relative time | Event |
| ---: | --- |
| `30.238 s` | Session ping response received |
| `51.425 s` | Analytics response received |
| `60.146 s` | Another session ping called |
| `60.230 s` | Ping response received successfully |
| `61.851 s` | Empty server message ID 0 discarded because no handler exists |
| `61.884 s` | Remote connection closed |

Static inspection confirms the logged `0` is a message ID, not a decoded
message type. Its application data is empty. It cannot carry an anti-cheat
challenge or rejection details. Only two of seven retained disconnect summaries
contain this warning, so it is not required for the failure.

## MRAC service discovery

The official game executable embeds protobuf service descriptors. They define
`FMracServiceWs::ClientRequest`, carrying an opaque byte request and opaque byte
response. Static inspection of the official game code found this flow:

```text
anti-cheat online
  -> game registers anti-cheat request delegate
  -> anti-cheat invokes delegate with opaque request
  -> game sends FMracServiceWs::ClientRequest
  -> game returns opaque response to anti-cheat
```

Matching client log strings describe a request attempt, received response, and
send failure. During the measured ACH 118 run, none appeared and no MRAC RPC was
sent. This places the missing event before the backend call: the registered game
bridge was never visibly invoked before disconnection.

## ACH 118 local request boundary

A narrow Wine file/synchronization trace and a read-only debugger sample moved
the boundary one step further inward:

- the live game environment contains both `GC_PIPE_NAME` and `GC_PROJECT_ID`,
  and the corresponding launcher named pipe exists and is available;
- only MY.GAMES launcher job processes were observed opening that pipe; the game
  process did not reference its token in the trace. This is an observation, not
  proof that the game is required to open it;
- the managed Steam `acclient64.dll` repeatedly loads, executes its protected
  exception path, and unloads;
- Wine handles the deliberate access violations used by that protected path;
  there is no unhandled crash; and
- a breakpoint at the official game's `Gate0018` wrapper return recorded
  `RAX=0` and `RSI=0`: the anti-cheat gate returned zero request bytes.

The resulting ACH 118 path is therefore:

```text
ACClient: Online
  -> game polls Gate0018
  -> protected anti-cheat DLL executes
  -> Gate0018 returns zero bytes
  -> no FMracServiceWs::ClientRequest is emitted
  -> backend validation/session lease closes near 60 seconds
```

This localizes the next investigation without interpreting or modifying any
opaque anti-cheat payload.

### Narrow relay control

A later ACH 118 control reduced Wine relay logging, isolated the game-process
lines, and covered the same failure boundary:

| Event | Time |
| --- | ---: |
| `ACClient: Online` | `0.000 s` |
| Backend WebSocket close `1006` | `63.031 s` |

After Online, a fresh Unreal `TAsyncThread` appeared every `3.00 s`. Each
sampled thread lasted about `22 ms`, received normal PE thread-attach and TLS
callbacks, then detached. The pattern continued through the backend close. The
sampled cycles made no visible pipe, socket, registry, cryptography, device, or
Steam API call. Protected execution can still make internal or direct system
calls that Wine relay does not expose, so this is a boundary rather than proof
that no dependency exists.

The clean follow-up also established that `GC_PROJECT_ID` is present in the
game's live PEB environment. The `GC_PIPE_NAME` endpoint is available at the
same time. Missing launcher environment fields are therefore not the current
explanation. The game process still made no observed open of that named pipe,
which remains a comparison point for a successful Steam Deck run.

Finally, the Wine base used by GE-Proton10-34 was compared with the current
Valve `proton_11.0` branch for loader, TLS, x86-64 exception, thread, and virtual
memory changes. No intervening change touched those paths. There is no obvious
newer base-Wine ntdll fix to port blindly; a new patch should follow a measured
behavioral difference on the successful Deck control.

## Steam Deck status query

The narrow Steam-client trace recorded one
`ISteamUtils::IsSteamRunningOnSteamDeck()` call, but its process ID belongs to
`MGL.exe`, not the game. It occurs among launcher identity and authorization
calls. The game process made no observed call to this API.

[Valve's API documentation](https://partner.steamgames.com/doc/api/isteamutils?l=english)
defines it as a query about whether Steam is running on a Steam Deck or another
SteamOS device. In this experiment it helps explain launcher/ACH selection; it
does not explain the later zero-byte `Gate0018` result after ACH 118 is already
active.

## Packet and transport result

Wine socket tracing exposed only TLS transport metadata. The separately visible
remote endpoint was the analytics service; no distinct anti-cheat heartbeat
socket appeared. MRAC is carried over the existing backend WebSocket. Packet
capture can confirm timing and connection closure, but the absent ACH 118 event
is local request generation before any MRAC packet exists.

An older MY.GAMES run logged repeated client-request failures. Its first failure
occurred `69.923 s` after anti-cheat online, after that run's backend was already
disconnected. The second ACH 118 control remained alive until `92.008 s` after
anti-cheat online without any request-attempt marker. This confirms a route
difference in request triggering; it does not establish a successful MRAC
exchange on ACH 77.

A fresh native-auth run selected ACH 120, not 77, and supplied a cleaner
comparison:

| Relative time | ACH 120 event |
| ---: | --- |
| `0.000 s` | Backend WebSocket established |
| `0.433 s` | Platform authorization response received |
| `0.557 s` | `ACClient: Online` |
| `1.593 s` | Initial session ping response received |
| `5.456 s` | `FMracServiceWs::ClientRequest` called |
| `5.539 s` | Normal MRAC RPC response received |
| `8.600 s` | Remote connection closed |

The request and response bodies remain opaque. A normal RPC response proves the
transport and game bridge worked; it does not prove the anti-cheat accepted the
response content. This establishes different failure stages:

- ACH 120 obtains an anti-cheat request, bridges it to the backend, receives a
  response, and is closed about three seconds later.
- ACH 118 never obtains or emits the request and is closed around 60–62 seconds.

## Binary and authorization routing details

The active Steam route launches the managed `13_2017027` copy, not the sibling
`War Robots Frontiers` copy. Its anti-cheat binaries differ even though both
clients report file version 4.47.0:

| File | Managed Steam SHA-256 | Sibling/MY.GAMES SHA-256 |
| --- | --- | --- |
| `acclient64.dll` | `e4f56458b55af561aaec46c1814efe104915331bc6ba105290ae3f6934e78e25` | `557edcc475da5d217f921bc0019d74562a46b8dacbef3cd38776e63533914374` |
| `aclaunchapi64.dll` | `05f0d683e81d037ef9d2beadaf269731e4d029f6adb2d9f5ff99d60e90c00cf2` | `860eeb939084963696118525df73f4d112d829731b18a0f0e98ff675797794e7` |

The sibling Steam and native MY.GAMES DLLs are byte-identical. The relevant game
bridge and backend-dispatch code is identical between game copies, so the static
control-flow finding still applies. The differing anti-cheat DLL pair is a
channel-specific variable worth comparing dynamically.

The normal Steam control's platform-auth request uses the default `MyGames`
primary enum and carries one additional `Steam` authorization request. The
resulting profile reports `BackendPlatform: Steam`. This shows the official
Steam route itself combines launcher and Steam authorization layers; the
presence of MY.GAMES-shaped primary auth is not evidence that the hybrid rewrite
caused the cutoff.

## Successful Steam Deck control

The extended payload-free collector captured a supported Steam Deck using game
build `24552611`, SteamOS `3.8.16`, kernel
`6.16.12-valve24.5-1-neptune-616-gb2f7cfe85e45`, and Proton compatibility
version `10.1000-105`.

Relative to `ACClient: Online`, the first MRAC request attempt appeared after
`4.704 s`, the first real `FMracServiceWs::ClientRequest` call after `9.706 s`,
and the first response after `25.846 s`. The request-attempt loop continued with
a median `5.009 s` interval. Six real MRAC calls received six responses while
17 session pings also completed. The backend remained connected for another
`497.624 s` before the user requested a normal exit. A second run remained
connected for more than nine minutes and also exited normally.

A later capture on the same build and Proton version remained healthy for
`16m30.221s`. Its first request attempt, call, and `697`-byte response occurred
at `4.632 s`, `4.633 s`, and `4.832 s` after AC Online. All nine MRAC calls and
all 33 session pings received responses. The run had no backend close and ended
normally.

Collector `sha-f94961b9220d` then captured a complete `11m20.319s` Deck control.
Its first real MRAC call and `697`-byte response occurred `4.772 s` and
`4.972 s` after AC Online. Six 4096-byte calls received six responses, the five
later responses were 521 bytes, all 23 session pings completed, and the run had
no collector gap or backend close. The captured `acclient64.dll` and
`aclaunchapi64.dll` hashes exactly match the desktop's active copies under
`13_2017027` on game build `24552611`; the differently hashed alternate local
copies are stale. Anti-cheat binary drift is therefore ruled out.

The second run did not contain the verbose-category markers, so its lack of
individual RPC events is a capture-quality limitation rather than evidence that
MRAC stopped. Neither Deck run contained a `gate_result` probe event. Process
environment and socket counts also require validation before interpretation.

This positive control strengthens the validation-lease interpretation: the
supported path begins the local anti-cheat request loop and completes MRAC well
before the failing desktop's approximately 60-second remote close.

## Interpretation

### Host platform-attestation trace

A payload-free `strace` control followed only file, firmware, `uname`, and
`sysinfo` operations on the game process. The thread that begins with
`acclient.cfg` activity at `ACClient: Online` immediately performed the
following sequence:

- probed `physicaldrive0` and the NVIDIA admin device;
- read the non-unique and serial-bearing SMBIOS/DMI fields plus machine ID;
- opened the Microsoft Platform Crypto Provider, which caused the patched Wine
  runtime to expose the host TPM EK public blob;
- enumerated per-logical-CPU maximum frequencies; and
- about `13.84 s` later requested `DMAR` then `IVRS` through the ACPI firmware
  API.

The backend closed `18.397 s` after connect, about `3.24 s` after the ACPI
query. The same candidate had already passed an API smoke test and returned the
authentic host values, so this is direct evidence that the protected worker
collects a multi-surface platform profile, but not proof of which property the
backend rejects. Supplying only Valve product strings would produce a profile
that conflicts with the host UUID/serial, TPM, GPU, CPU, storage, and ACPI
surfaces.

The collector now records the complete non-secret comparison set once per run:
model-level DMI fields, PCI IDs, logical CPU count, ACPI table presence/sizes,
and presence/readability flags. It deliberately excludes serial values,
machine-ID values, firmware-table contents, TPM blobs, and keys.

The fresh Deck profile reports Valve/Aerith/Jupiter DMI, readable product and
board serial fields but no chassis serial, eight logical CPUs, one AMD
`1002:163f` GPU, TPM devices, and a 76-byte TPM2 table. It has neither DMAR nor
IVRS. The desktop reports ASUS/ROG DMI, no readable serial fields, 16 logical
CPUs, AMD `1002:164e` plus NVIDIA `10de:2702`, the same TPM device and TPM2-table
presence, no DMAR, and a 200-byte IVRS table. `physicaldrive0` and
`nvadmindevice` are absent and machine ID is readable on both systems. These
are measured profile differences, not proof that any individual field causes
the rejection or a set of values to imitate.

Decoded structural comparison of six accepted Deck pairs and four rejected
desktop pairs found no transport framing defect. Every request is 4096 bytes,
has approximately 7.95 bits/byte of entropy, is incompressible, and shares only
the same four-byte framing prefix; all remaining cross-path byte matches occur
at the random-byte rate. Responses have approximately 7.7 bits/byte of entropy
and no stable positions. Both accepted and rejected runs produced 697-byte and
681-byte initial responses, and one rejected desktop response returned in only
83 ms. Length, padding, visible structure, and response latency therefore do
not distinguish acceptance. The meaningful difference is inside protected
per-run material or backend validation state, where blind byte patching is not
technically justified.

An AMD-only rendering control hid every non-selected Vulkan adapter and verified
that Unreal chose the Raphael iGPU as D3D12 adapter 0. The WebSocket still
closed with code 1006 after `18.399 s`; the equivalent NVIDIA control closed
after `18.397 s`. The `2 ms` difference rules out GPU vendor or adapter choice
as the cause of this boundary.

A targeted `+ncrypt` trace then recorded the complete Platform Crypto Provider
call sequence. The protected worker opened `Microsoft Platform Crypto
Provider`, read the `PCP_EKPUB` property containing the authentic 283-byte host
TPM endorsement public key, and freed the provider. It did not enumerate or
open a private key and did not call `NCryptSignHash`, decrypt, or encrypt. This
rules out a missing NCrypt signing implementation for this run, although it
does not prove that the backend ignores consistency among the public EK, DMI,
ACPI, CPU, storage, and other platform facts.

### Valve Proton callback finding

The first exact-Valve-Proton experiment did not reach the earlier 60-second
boundary. It connected to the backend and then crashed during login. Wine's
module diagnostics showed all four stale TLS directory pointers in the managed
`acclient64.dll` being repaired, immediately followed by exception-stack
exhaustion.

Static inspection found that the TLS callback array itself was now reachable,
but both function addresses stored in it still pointed into the DLL's preferred
image range. The opaque relocation table had not adjusted them, and Wine calls
callback entries directly. The narrow loader experiment was therefore extended
to rebase such entries under mapped-image, original-image-range, and 64-entry
bounds.

The follow-up trace confirmed that all four TLS directory pointers and both
callbacks were repaired. The first subsequent fault was an execute access
violation at `0x180611d49`, another address within `acclient64.dll`'s preferred
image but outside Wine's actual high mapping. This repeated through Wine's
exception path 1,899 times until the thread stack overflowed. The wrapper-free
launch reproduced the failure, ruling out `run-steamos.sh` as its cause.

The same trace showed Wine's data-only `Normaliz.dll` mapped at
`0x180000000` before `acclient64.dll`; both files declare that preferred base.
The next controlled build moved only 64-bit `Normaliz.dll`'s requested mapping
to `0x190000000` and lets exact `acclient64.dll` try `0x180000000`. This behavior
requires `WRF_PREFER_ACCLIENT_BASE=1` and otherwise remains inactive.

That preferred-base run reached login without an access violation or recursive
exception failure. Relative to `ACClient: Online`, its first request attempt and
real `FMracServiceWs::ClientRequest` call occurred after `4.897 s` and
`4.898 s`. RPC id 47 returned after `8.551 s`, and ACClient accepted a
`697`-byte server response. Backend close `1006` followed only `2.967 s` later,
or `16.416 s` after AC Online. There was no RPC error or failure; ordinary RPC
responses continued after the MRAC response and before closure.

This supersedes the zero-byte Gate boundary for the preferred-base runtime. It
does not invalidate the earlier relocated-image observation; instead it shows
that allowing the protected DLL to execute at its preferred base restores the
request-generation path and moves the remaining failure beyond the completed
MRAC exchange.

The same preferred-base change was then rebased onto the custom
GE-Proton10-34 Wine tree. The verified candidate mapped `acclient64.dll` at
`0x180000000` and `Normaliz.dll` at `0x190000000`. Relative to AC Online, RPC id
47 was called after `4.896 s`, returned after `13.405 s`, and close `1006`
followed after `15.922 s` (`2.517 s` after the response). This independent GE
result confirms that the restored Gate0018 exchange is caused by the narrow
image-base behavior rather than a Valve-only runtime component.

The local post-response probe then observed the preferred-base GE candidate
from AC Online through backend close and process shutdown. It recorded 30,861
first-chance access violations on one persistent anti-cheat worker thread, but
no TLS-callback or `DllMain` exception; every observed `DllMain` returned
success. Disassembly of all 17 exception RVAs shows deliberate byte reads at
page boundaries inside repeated scanner loops. The bursts begin `4.376 s`
after AC Online, recur roughly once per second, and continue for `6.714 s`
after backend close. This is handled exception-driven memory scanning rather
than an unhandled Wine fault, and the scanner remains active after rejection.

That run closed `24.424 s` after AC Online. Its game log did not have the
verbose MRAC category enabled, so the exact call/response boundary is unknown;
the absence of an MRAC line is not evidence that no MRAC exchange occurred.
The captured callback and exception behavior nevertheless rules out a visible
TLS, `DllMain`, or exception-dispatch failure at the close boundary. It does
not prove that the scanner's attestation result caused the rejection.

A second probe run added the native access type and fault address. All 34,670
exceptions were reads on one worker thread, and all 34,670 targets were unique.
They form a forward virtual-address sweep from `0x126fc` through `0x8208fa8e`,
executed as 26 short slices roughly one second apart. The sweep began `4.395 s`
after AC Online. It paused from `12.578 s` through `16.800 s`; backend close
`1006` occurred at `16.319 s`, during that pause, and scanning resumed `0.481 s`
after the close. It continued until the user exited at about `33.2 s`.

The stable scan start (within `20 ms` of the first probe run) and matching
four-second pause boundaries (within `16 ms`) contrast with close times that
differ by `8.105 s`, ruling out a fixed local scanner timer. The close also does
not wait for the address sweep to finish. This makes a scanner-speed workaround
unjustified:
the useful comparison is now the target ranges, slice timing, and address-space
layout on a successful Deck run. Although `-LogCmds` appeared on this run's
command line, Unreal did not report raising those categories, so the exact MRAC
response timestamp remains unavailable.

**Observed:**

- the older relocated ACH 118 path emits no MRAC request and closes near 60
  seconds after `Gate0018` returns zero bytes;
- the preferred-base ACH 118 path completes a real MRAC call/response with a
  `697`-byte client response, then closes `2.967 s` later;
- the GE-Proton10-34 port independently completes the same RPC boundary and
  closes `2.517 s` after its response;
- successful Deck controls also begin the request loop near five seconds, but
  keep the backend connected after their MRAC responses;
- normal session pings and other RPCs succeed on both the working Deck and the
  failing desktop paths;
- the active Deck and desktop anti-cheat DLLs have identical SHA-256 values on
  game build `24552611`;
- forcing the authentic AMD iGPU changes neither the failure nor its timing;
- the Platform Crypto Provider access reads only the public TPM EK property and
  performs no NCrypt private-key operation;
- the game contains and registers an anti-cheat-to-backend MRAC bridge;
- both GameCenter environment fields are present and the named pipe is
  available; and
- a `3.00 s` transient-thread/TLS cycle continues through the close without a
  visible external API call from the sampled cycle.

**Inferred:**

- the preferred-base mapping repairs the loader and request-generation failure;
- the remaining preferred-base failure is validation of the completed
  anti-cheat exchange or its subsequent state transition, not missing MRAC
  WebSocket transmission;
- the earlier 60-second boundary likely expired a validation/session lease,
  while the current three-second post-response close is a prompt rejection; and
- the next useful comparison is protected execution around the first request
  and response on Deck versus desktop.

**Unknown:**

- what differs inside the opaque request or response-processing state between
  the accepted Deck exchange and rejected desktop exchange;
- whether the Deck opens the advertised GameCenter pipe from the game process;
  and
- which memory ranges and environment properties feed the scanner's
  attestation result, and whether they govern post-response validation.

## Next controlled tests

The collector now permanently captures its complete safe diagnostic profile in
one run. In addition to the existing lifecycle, RPC/transport, process,
runtime, hash, liveness, and structured Proton-probe events, it extracts the
exact Base64 value only from `FMracServiceWs::ClientRequest` calls and
responses. Each blob includes its verified decoded size and SHA-256; unrelated
RPC bodies and source lines remain excluded.

The agent also tails `~/steam-1491000.log` directly when an approved
`WRFPROBE` runtime is active. It normalizes FILETIME, module/thread/callback
lifecycle, exception access type, RVA, and fault target without uploading the
source Proton log. The dashboard reports probe events, faults, unique targets,
and timed scan slices.

The local runtime sequence must preserve the known working route before testing
the measured Deck profile. `GE-Proton10-34-WRF-DeckCompare` layers the platform
controls on the preferred-base GE10 line that produced ACH 118 and a completed
RPC id 47 MRAC exchange. ACH 118 plus that call/response is the acceptance gate
for every stage; ACH 77 is a route regression, not a platform comparison. The
runtime provides three cumulative, trackable stages:

1. Run the default `acpi` stage: authentic host TPM and ACPI data, except DMAR
   and IVRS are absent as on Deck. This isolates the only observed ACPI-table
   difference while preserving a real TPM EK.
2. If the same post-response rejection remains, run `dmi`: add the measured
   Valve/Aerith/Jupiter model fields and the Deck's product/board-readable,
   chassis-unreadable shape using locally generated test identities. It does
   not use a real Deck serial.
3. If unchanged, run `full`: add Wine's native eight-logical-CPU topology
   override. GPU spoofing is excluded because the AMD-only and NVIDIA controls
   already produced the same boundary within 2 ms.
4. If ACH 118 and RPC id 47 remain intact, repeat `full` with Steam and the game
   in a private mount namespace whose `/etc/os-release` reports the measured
   SteamOS 3.8.16 identity. This is cumulative but does not modify the host OS.
5. Compare exception codes/addresses, transient-thread lifetime, and TLS
   callback order around the first MRAC request and response on Deck and this
   host when a matching Deck probe becomes available.
6. Compare whether the successful Deck game process opens `GC_PIPE_NAME` and at
   what point relative to AC Online and the first MRAC request.
7. Ask the developer/backend operator to correlate the accepted Deck RPC at
   `2026-08-10T15:25:16.618Z` and a rejected desktop RPC with the internal
   validation decision, or supply an explicitly test-only allowlist/profile.
8. Patch Wine further only after a specific incompatible behavior is identified and can
   be reproduced independently of protected payload contents.

The cumulative SteamOS-identity control completed on the GE10 line. The live
game process saw `ID=steamos`, `VERSION_ID=3.8.16`, and
`VARIANT_ID=steamdeck`; Wine exposed eight logical CPUs; ACH 118 remained
selected; and RPC id 47 completed its real MRAC call and response. Relative to
AC Online, the call occurred at `4.895 s`, the response at `13.414 s`, and close
`1006` at `16.081 s` (`2.667 s` after the response). The host reverted to its
real OS identity when the process namespace exited. This rules out the tested
`os-release` identity as a sufficient fix while preserving the correct route.

Packet capture remains useful for TCP/TLS timing and endpoint confirmation. It
cannot identify the missing RPC without TLS decryption, while official
application metadata already answers that question with less exposure.
