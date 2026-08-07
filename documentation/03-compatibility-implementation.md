# Compatibility implementation

## Distributed Steam runtime

The repository's supported artifact is `GE-Proton10-34-WRF-TLS`, installed by
`Steam/setup.sh`. Its source provenance and patch are in `Steam/source/`.

Required Steam launch options:

```bash
WINEDLLOVERRIDES="GCLay.dll=d;GCLay64.dll=d" SteamDeck=1 %command%
```

The DLL overrides avoid a GCLay/D3D12 crash. The Wine ntdll patch repairs
relocated PE TLS pointers that still refer to the image's preferred base. The
guard is intentionally narrow and is not anti-cheat-specific logic.

Current status: this gets through launch, anti-cheat initialization, and account
loading. It does not prevent the later remote backend close.

## Laboratory ACH 77 compatibility runtime

The separate `GE-Proton11-3-WRF-DMAGuard` work is a research runtime, not yet a
distributed end-user setup. It implements missing Windows behavior queried by
the native route:

- ACPI firmware table support with a captured real host table;
- `SystemDmaGuardPolicyInformation` class 202;
- the real host TPM endorsement **public** key through `PCP_EKPUB`;
- `NCryptDecrypt` through existing BCrypt behavior;
- status/length-only Schannel diagnostics.

The implementation never provides a private TPM key, invents host security
state, or alters network payloads. It was enough for ACH 77 to reach anti-cheat
Online and the hangar, but not enough to retain the backend session.

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
