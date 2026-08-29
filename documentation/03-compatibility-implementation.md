# Compatibility implementation

## Distributed Steam runtime

The repository's supported artifact is `GE-Proton10-34-WRF-TLS`, installed by
`Steam/setup.sh`. Its source provenance and patch are in `Steam/source/`.

Required Steam launch options:

```bash
WINEDLLOVERRIDES="GCLay.dll=d;GCLay64.dll=d" SteamDeck=1 %command%
```

The DLL overrides avoid a GCLay/D3D12 crash. The bundled Wine ntdll patch
repairs relocated PE TLS directory pointers that still refer to the image's
preferred base. The guard is intentionally narrow and is not
anti-cheat-specific logic. Later experimental candidates add bounded callback
repair and preferred-base mapping without changing this packaged runtime.

Current status: this gets through launch, anti-cheat initialization, and account
loading. It does not prevent the later remote backend close.

## Valve Proton 10.0 comparison runtime

An earlier successful Steam Deck capture reported compatibility version
`10.1000-105`, which maps to Valve Proton `proton-10.0-4b`. The current
same-build Deck control reports Valve Proton `11.0-100`, so Proton 10 is now a
historical A/B rather than the current baseline. The source helper in
`Steam/source/build-proton-10-wrf.sh` applies the existing narrow ntdll patch to
that exact Wine revision and overlays only the resulting Unix ntdll modules on
the installed official runtime. Apart from identifying registration and
provenance metadata, no other runtime component changes. This is the smallest
controlled comparison against the Deck software baseline; it remains an
experimental local build until a game run confirms its behavior.

The first comparison build repaired the managed anti-cheat DLL's TLS directory
but not the callback targets stored in that directory's callback array. The
next build repaired those callbacks, but the DLL then executed another stale
protected-code pointer, `0x180611d49`, and recursively exhausted its exception
stack. Wine's builtin `Normaliz.dll` had already claimed `acclient64.dll`'s
preferred base at `0x180000000`.

The current comparison build adds an opt-in, exact-name loader experiment. With
`WRF_PREFER_ACCLIENT_BASE=1`, it moves only 64-bit `Normaliz.dll`'s requested
mapping to `0x190000000` and lets only `acclient64.dll` first try its original
base. It falls back to Wine's relocation path if the requested address is not
available. The tested command is:

```bash
PROTON_LOG=1 WINEDEBUG="warn+module" WRF_PREFER_ACCLIENT_BASE=1 WINEDLLOVERRIDES="GCLay.dll=d;GCLay64.dll=d" SteamDeck=1 %command%
```

This reaches the hangar without the stale execute fault. It also restores a real
ACH 118 MRAC exchange: the first request is called `4.898 s` after AC Online and
a `697`-byte response is handed to ACClient `8.551 s` later. The backend closes
`2.967 s` after that handoff. The preferred-base behavior is therefore a loader
and request-generation fix, not a complete backend-validation fix.

## GE-Proton10-34 preferred-base comparison runtime

The same opt-in loader behavior is rebased onto the Wine revision used by the
custom GE runtime in
`Steam/source/wrf-prefer-acclient-image-base-ge10.patch`. The local comparison
tool is registered separately as `GE-Proton10-34-WRF-PreferredBase`, preserving
the distributed runtime for A/B tests.

This GE candidate maps the two images at the intended bases and restores a real
Gate0018 MRAC call/response. From AC Online, the call occurred after `4.896 s`,
the response after `13.405 s`, and close `1006` after `15.922 s`. Its behavior
therefore agrees with the exact Valve candidate: the loader/request-generation
failure is fixed, while the completed exchange is still rejected remotely.

## Local post-response instrumentation runtime

`GE-Proton10-34-WRF-PostResponseProbe` is a separate local research tool based
on the preferred-base candidate. Its PE-side ntdll adds the disabled-by-default
`wrfprobe` channel. When enabled, it emits structured, wall-clock-correlated
events only for `acclient64.dll`: module load/unload, thread attach/detach, TLS
callback and `DllMain` entry/return/exception, and exception dispatch whose
address lies inside the image. This supplies the exception, callback-order, and
transient-thread evidence needed before deciding whether another behavior patch
is justified, without the timing and storage cost of global `seh` or `relay`.

The source patch, local build helper, launch options, and correlation analyzer
are documented in `Steam/source/README.md`.

## Laboratory GE-Proton11-6 MRAC compatibility runtime

The current local `GE-Proton11-6-WRF-MRAC` work is a research runtime, not yet a
distributed end-user setup. It accumulates the measured compatibility behavior
queried by the native route:

- ACPI firmware table support with a captured real host table;
- `SystemDmaGuardPolicyInformation` class 202;
- the real host TPM endorsement **public** key through `PCP_EKPUB`;
- `NCryptDecrypt` through existing BCrypt behavior.

It also retains the exact-name preferred-base loader behavior, stale TLS-pointer
repair, and ACH launch-mode probes. The implementation never provides a private
TPM key, invents host security state, or alters network payloads. The current
build verified preferred-base loading, DMA Guard support, and a successful
283-byte `PCP_EKPUB` read. ACH 77 reached anti-cheat Online, but generated no
MRAC RPC and lost the backend 8.500 seconds later. The same updated client also
failed to generate MRAC under GE-Proton10-34 preferred-base, so the present
request-generation failure is not specific to Wine 11.

## GE-Proton10-34 host-security control

`GE-Proton10-34-WRF-HostSecurity` ports the same authentic-host surfaces onto
the working preferred-base/post-response-probe line. It reads explicit local
ACPI snapshots, exposes the host TPM endorsement public key, and implements the
one-byte DMA Guard policy result. A probe A/B confirms that the candidate
changes only those previously unsupported Windows APIs.

The controlled game run reached `ACClient: Online` and loaded the hangar, then
the backend closed `17.821 s` after WebSocket connect. This is effectively the
same prompt post-response rejection as the preferred-base control, so ACPI,
DMA Guard, and TPM EK API availability alone are not the fix.

## Launcher-job observation

Local lab files under `<private-workspace>/launcher-research/`
include:

- `capture-loop.sh`: repeatable launcher/capture cycle;
- `analyze_pipe_trace.py`: structural trace analysis and sanitization;
- a custom Wine `kernelbase/file.c` `WRFPIPE` capture/rewrite path;
- `selftest_pipe.c` and `selftest_rewrite.c` regression checks.

The mechanism observes the MY.GAMES launcher's named-pipe
`GCJobGameLaunch` message. The successful hybrid test retained the launcher's
own account fields while substituting the official Steam executable, directory,
anti-cheat launch library, app ID, ACH value, and launch flags.

This is a research proof, not an end-user third-party launcher. Any future tool
must continue to let the official launcher establish authentication and must
not store, request, log, or transmit credentials itself.

## Local lab helpers

Other useful, non-distributed scripts currently live under
`<private-workspace>/`:

- `launch-wrf-mygames-deck.sh`
- `prepare-wrf-proton-dmaguard.sh`
- `snapshot-wrf-acpi-tables.sh`
- `snapshot-wrf-tpm-ek.sh`
- `stop-wrf-mygames-game.sh`
- `run-wrf-steam-cycle.sh`
- `run-wrf-mygames-ge10-cycle.sh`
- `wrf-steam-mygames-auth-cache.sh`

These paths document the current workstation layout only. They may contain
machine-specific assumptions and should not be copied into the public setup
without a separate security and portability review.
