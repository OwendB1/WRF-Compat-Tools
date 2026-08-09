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
normally. It also reported an `aclaunchapi64.dll` SHA-256 that differs from both
local copies; its 104 MiB `acclient64.dll` exceeded the collector's former
64 MiB hash ceiling, so complete binary identity remains unresolved.

The second run did not contain the verbose-category markers, so its lack of
individual RPC events is a capture-quality limitation rather than evidence that
MRAC stopped. Neither Deck run contained a `gate_result` probe event. Process
environment and socket counts also require validation before interpretation.

This positive control strengthens the validation-lease interpretation: the
supported path begins the local anti-cheat request loop and completes MRAC well
before the failing desktop's approximately 60-second remote close.

## Interpretation

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
  and response on Deck versus desktop, plus exact anti-cheat binary identity.

**Unknown:**

- what differs inside the opaque request or response-processing state between
  the accepted Deck exchange and rejected desktop exchange;
- the successful Deck's `acclient64.dll` SHA-256 (the old collector's 64 MiB
  ceiling skipped the 104 MiB file);
- whether the Deck opens the advertised GameCenter pipe from the game process;
  and
- which exception, unwind, TLS, timer, worker, or IPC behavior governs the
  post-response validation state.

## Next controlled tests

The collector now permanently captures its complete safe diagnostic profile in
one run. In addition to the existing lifecycle, RPC/transport, process,
runtime, hash, liveness, and structured Proton-probe events, it extracts the
exact Base64 value only from `FMracServiceWs::ClientRequest` calls and
responses. Each blob includes its verified decoded size and SHA-256; unrelated
RPC bodies and source lines remain excluded.

1. Compare both managed anti-cheat DLL hashes under the same game build on Deck
   and desktop.
2. Compare the MRAC request and response blob lengths, hashes, and byte-level
   structure between a successful Deck and failing preferred-base desktop run.
3. Compare exception codes/addresses, transient-thread lifetime, and TLS
   callback order around the first MRAC request and response on Deck and this
   host.
4. Compare whether the successful Deck game process opens `GC_PIPE_NAME` and at
   what point relative to AC Online and the first MRAC request.
5. Patch Wine only after a specific incompatible behavior is identified and can
   be reproduced independently of protected payload contents.

Packet capture remains useful for TCP/TLS timing and endpoint confirmation. It
cannot identify the missing RPC without TLS decryption, while official
application metadata already answers that question with less exposure.
