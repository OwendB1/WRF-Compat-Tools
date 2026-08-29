# Runtime source and provenance

`GE-Proton10-34-WRF-TLS` is based on GE-Proton10-34 with one WRF-specific Wine loader patch.

## Revisions

- GE-Proton source: [`GE-Proton10-34`](https://github.com/GloriousEggroll/proton-ge-custom/tree/GE-Proton10-34), commit `721dd76896b434fe3c1328ea533e0b25b4af04d5`
- Wine submodule: [ValveSoftware/wine](https://github.com/ValveSoftware/wine), commit `1729f00e17e879f98f9df1f2bca86bc5d21a65df`
- Distributed runtime modification:
  [`ntdll-rebase-stale-tls-pointers-ge10.patch`](ntdll-rebase-stale-tls-pointers-ge10.patch)
- Experimental modifications:
  - [`ntdll-rebase-stale-tls-pointers.patch`](ntdll-rebase-stale-tls-pointers.patch), extending the repair to bounded callback entries
  - [`wrf-prefer-acclient-image-base.patch`](wrf-prefer-acclient-image-base.patch), based on Valve Proton 10.0
  - [`wrf-prefer-acclient-image-base-ge10.patch`](wrf-prefer-acclient-image-base-ge10.patch), rebased on GE-Proton10-34

The distributed patch was applied from `patches/protonprep-valve-staging.sh`
after the existing pending Wine hotfixes were selected and before the Wine
build. The recorded hashes below identify that packaged revision exactly.

## Runtime verification

Key hashes from the packaged build:

```text
47e9fddf687792d041b2517e7ec24e7b8c0bc393d65b80261237a7c802fa893e  files/lib/wine/x86_64-unix/ntdll.so
d44cc662a943794089eb34965ca6f9fd5de5b0204af87748d2d8ce60e9ca1713  files/lib/wine/x86_64-windows/ntdll.dll
560ff791703000aaa5b593190ce0ca111d9b98ad6ff4426cbe49c225786b9535  proton
```

All upstream submodule revisions are recorded by the GE-Proton commit. Component license files are preserved in the packaged runtime.

## GE-Proton10-34 preferred-base candidate

The preferred-base experiment was ported to the custom GE source and installed
separately as `GE-Proton10-34-WRF-PreferredBase`; it does not overwrite the
distributed `GE-Proton10-34-WRF-TLS` runtime. Only the candidate's 64-bit Unix
`ntdll.so` differs from that base runtime. Enable the behavior with the same
`WRF_PREFER_ACCLIENT_BASE=1` launch option used by the Valve comparison.

The verified GE run mapped `acclient64.dll` at `0x180000000` and 64-bit
`Normaliz.dll` at `0x190000000`. Relative to `ACClient: Online`, RPC id 47
called `FMracServiceWs::ClientRequest` after `4.896 s`, returned after
`13.405 s`, and was followed by backend close `1006` after `15.922 s`. The GE
port therefore restores Gate0018 request generation and reproduces the Valve
post-response rejection; it does not restore the successful Deck session.

## GE-Proton10-34 post-response probe

[`ntdll-wrf-post-response-probe-ge10.patch`](ntdll-wrf-post-response-probe-ge10.patch)
adds a dedicated `wrfprobe` Wine debug channel to the preferred-base candidate.
It records only `acclient64.dll` module and thread lifecycle, TLS callback and
`DllMain` entry/return, and exceptions dispatched from inside that image.
Access-violation events include the native access type and fault target. Every
event includes Windows FILETIME and thread ID, so it can be aligned with the
game's first MRAC response without enabling the high-volume `seh` or `relay`
channels. It does not record request or response contents.

The local helper builds the 64-bit PE `ntdll.dll` in the existing configured GE
tree and installs a separate `GE-Proton10-34-WRF-PostResponseProbe` tool:

```bash
./Steam/source/build-ge-proton-10-post-response-probe.sh
```

After restarting Steam, select that tool and use:

```bash
PROTON_LOG=1 WINEDEBUG="warn+module,+wrfprobe" WRF_PREFER_ACCLIENT_BASE=1 WINEDLLOVERRIDES="GCLay.dll=d;GCLay64.dll=d" SteamDeck=1 %command%
```

Analyze the resulting local logs with:

```bash
./Steam/source/analyze-wrf-post-response.py \
  --proton-log "$HOME/steam-1491000.log" \
  --game-log "$HOME/.local/share/Steam/steamapps/compatdata/1491000/pfx/drive_c/users/steamuser/AppData/Local/WRFrontiers/Saved/Logs/WRFrontiers.log"
```

The report shows probe events from five seconds before through five seconds
after the first MRAC response, derives matching transient-thread lifetimes, and
summarizes the full run's fault-target sweep as timed address slices.
If verbose MRAC logging is absent, it falls back to the backend close and says
so in the report. `--reference` can select the close or AC Online explicitly;
use `--game-time-offset` only if the game log is demonstrably not UTC.

## Sanctioned Gate0018 boundary probe

`run-wrf-gate0018-probe.sh` attaches the observation-only GDB Python probe to
the exact supported game/DLL hashes. Start a normal measured Steam cycle, then
run:

```bash
./Steam/source/run-wrf-gate0018-probe.sh
```

It waits for the game and `acclient64.dll`, verifies both hashes, uses hardware
breakpoints/watchpoints, and detaches after the first non-empty request or four
empty polls. Artifacts are written with private permissions under
`/home/owendb/Games/wf-frontiers/gate0018-probes`; they are never uploaded by
the collector. Set `WRF_GATE_PROBE_GDB` and `WRF_GATE_PROBE_GDB_DATA` when GDB
is installed outside `PATH`.

The default pass discovers the per-process internal copy source and records the
exact `acclient64.dll` writer RVA. A same-process follow-up can watch an already
known, still-live source buffer with:

```bash
WRF_GATE_PROBE_SOURCE_ADDRESS=0x... ./Steam/source/run-wrf-gate0018-probe.sh PID
```

Source addresses are process-local and must not be carried into a new run. The
probe does not modify or replay request data.

To capture the platform inputs before the first protected request, start the
probe before launching the official Steam route:

```bash
WRF_GATE_PROBE_MODE=early-acquisition ./Steam/source/run-wrf-gate0018-probe.sh
```

This mode discovers the game by its mapped executable even after Wine renames
the process to `GameThread`. It records only API calls whose live stack reaches
`acclient64.dll`, saves bounded input/output buffers and call stacks with mode
`0600`, and detaches at the same 4096-byte handoff. Completed return traps are
disabled rather than deleted to avoid a GDB 16 Python-breakpoint cleanup bug.

## GE-Proton10-34 authentic host-security candidate

[`wrf-host-security-apis-ge10.patch`](wrf-host-security-apis-ge10.patch) adds
real-host ACPI table reads, `SystemDmaGuardPolicyInformation`, and TPM EK public
key exposure to the post-response-probe line. It does not provide private TPM
state or a fabricated Deck identity. Build and install the separate candidate
with:

```bash
./Steam/source/build-ge-proton-10-host-security.sh
```

The builder requires existing readable real-host `TPM2` ACPI and TPM EK public
snapshots and refuses to overwrite an existing candidate. Its Windows API
smoke test changes DMA Guard from unsupported to disabled, exposes the real
TPM2/IVRS tables, and returns a validated 283-byte RSA EK public blob. In the
game, ACClient still reached Online before the backend closed `17.821 s` after
connect, so these API surfaces are necessary comparison points but not a
complete solution.

## GE-Proton10-34 Deck-comparison candidate

The earlier successful Deck control rules out anti-cheat binary drift and shows
the measured platform differences directly: the Deck has no DMAR or IVRS table,
uses Valve/Aerith/Jupiter SMBIOS fields with product and board serials readable,
and exposes eight logical CPUs. The desktop has a 200-byte IVRS table, different
SMBIOS fields, and 16 logical CPUs. GPU selection was already ruled out by an
AMD-only control.

[`wrf-deck-profile-ge10.patch`](wrf-deck-profile-ge10.patch) adds an explicit
SMBIOS source directory to the GE-Proton10-34 Wine tree. The builder layers it
on `GE-Proton10-34-WRF-HostSecurity`, which already includes the preferred-base
fix that produced ACH 118 and RPC id 47 plus the authentic host-security APIs.
It embeds no identity and installs a separate runtime:

```bash
./Steam/source/build-ge-proton-10-deck-compare.sh
```

The runtime exposes three cumulative stages through `WRF_DECK_COMPARE_STAGE`:

- `acpi` (default): preferred-base request generation, authentic host TPM EK,
  and a copy of the authentic host ACPI snapshot with DMAR/IVRS absent;
- `dmi`: `acpi` plus the measured non-unique Valve/Aerith/Jupiter model fields,
  locally generated test product/board identities, and no chassis serial; and
- `full`: `dmi` plus Wine's native eight-logical-CPU topology override.

For this historical GE10 experiment, retain a real MRAC call/response before
judging the platform controls; the numeric RPC ID is not fixed. Run `acpi`
first so the IVRS difference remains isolated, then set the next stage only if
request generation is retained:

```bash
WRF_DECK_COMPARE_STAGE=dmi %command%
WRF_DECK_COMPARE_STAGE=full %command%
```

The test profile contains no Deck serial, TPM private key, or modified network
payload. The collector records presence of every comparison control so the
result cannot be confused with older runs.

The final cumulative userspace control can also present the measured Deck
SteamOS version to Steam and every child process without changing the host's
real `/etc/os-release`. Stop Steam first, then launch the normal cycle inside a
private user/mount namespace:

```bash
./Steam/source/run-with-steamos-release.sh COMMAND [ARG ...]
```

[`steamos-deck-os-release`](steamos-deck-os-release) identifies SteamOS 3.8.16,
matching the successful Deck capture. The bind mount exists only for that
command tree; after it exits, the host still reports its original OS.

Pressure Vessel reserves its own `/etc` and `/usr`, so the outer namespace alone
changes Steam's view but not the game container's runtime identity. Build and
select the final layered candidate to repeat the same read-only bind after
Pressure Vessel starts:

```bash
./Steam/source/build-ge-proton-10-steamos-compare.sh
```

`GE-Proton10-34-WRF-DeckCompare-SteamOS` is based on the GE10 DeckCompare
candidate, retaining its preferred-base, host-security, ACPI, DMI, and CPU
controls. Its Proton entry point creates one nested mount namespace before Wine
starts; neither the shared Steam Linux Runtime nor the host file is modified.

The cumulative local control verified the live game process as SteamOS 3.8.16
with `VARIANT_ID=steamdeck`, eight logical CPUs, ACH 118, and a completed
`FMracServiceWs::ClientRequest` call/response on RPC id 47. The call occurred
`4.895 s` after AC Online, the response at `13.414 s`, and backend close `1006`
followed `2.667 s` later. The OS identity surface therefore does not explain the
remaining prompt post-response rejection.

## Valve Proton 10.0 A/B candidate

An earlier Steam Deck capture identified Steam compatibility version
`10.1000-105`. The current same-build Deck control instead reports Valve Proton
`11.0-100`; Proton 10 remains a historical comparison rather than the current
software baseline. The matching installed Valve 10 runtime reports
`proton-10.0-4b`. Its exact source revisions are:

- Proton tag `proton-10.0-4b`, commit `e91ca2be0df2cef4c230cbbc0b86604d73a0bbf6`;
- Wine commit `b8fdff8e1f855b5276ec4ddca0f31b2792554322`; and
- the experimental TLS relocation patch above, including its bounded repair of
  stale callback entries; and
- an opt-in preferred-base experiment in
  [`wrf-prefer-acclient-image-base.patch`](wrf-prefer-acclient-image-base.patch).

[`build-proton-10-wrf.sh`](build-proton-10-wrf.sh) builds only Wine's `ntdll`
module after running Valve's generated-header preparation target, copies the
locally installed official Proton 10.0 runtime, and replaces only its 32- and
64-bit Unix `ntdll.so` files. Every other runtime component therefore remains
byte-for-byte identical to Valve's installed build; only the tool-registration,
version-label, and provenance metadata also differ. The result is installed as
`Proton-10.0-4b-WRF-TLS` for a controlled A/B test:

```bash
./Steam/source/build-proton-10-wrf.sh
```

The script refuses to overwrite an existing candidate or use unexpected
Proton/Wine revisions. This candidate does not replace the distributed GE
runtime until the game test establishes that it launches and changes the MRAC
behavior.

The first Valve-runtime test reached backend login but then crashed while
loading the managed `acclient64.dll`. Its TLS directory had four absolute
pointers and two callback entries omitted from an opaque relocation table. The
initial patch repaired the directory, but Wine then called the two callbacks at
their stale preferred-base addresses and exhausted the exception stack. The
callback extension repaired those entries, but the next run still executed
address `0x180611d49`: another protected-code pointer inside the DLL's preferred
image range that its relocation table did not cover. Wine's builtin
`Normaliz.dll` already occupied the DLL's preferred base, `0x180000000`.

The second experiment is therefore enabled only by
`WRF_PREFER_ACCLIENT_BASE=1`. For exact 64-bit `Normaliz.dll`, it tries
`0x190000000`; for exact `acclient64.dll`, it bypasses Wine's dynamic address
choice and first tries the DLL's own `0x180000000` base. If either address is
unavailable, normal relocation behavior resumes. No DLL is replaced or
rewritten by this experiment.

Use this launch command. `+seh` is deliberately omitted: the previous run
produced an 840 MB log after the recursive exception consumed the thread stack.

```bash
PROTON_LOG=1 WINEDEBUG="warn+module" WRF_PREFER_ACCLIENT_BASE=1 WINEDLLOVERRIDES="GCLay.dll=d;GCLay64.dll=d" SteamDeck=1 %command%
```

The preferred-base run reached login without the stale execute fault. Relative
to `ACClient: Online`, it attempted and called
`FMracServiceWs::ClientRequest` after `4.897 s`/`4.898 s`, received the RPC and
passed a `697`-byte response to ACClient after `13.449 s`, then received backend
close `1006` after `16.416 s`. The loader experiment therefore restores the
MRAC request path but does not make the resulting validation exchange succeed.

## GE-Proton11-6 ACH boundary and consumer probe

Current launcher contracts reproduce ACH 77, 87, 120, and 128. A supported
same-build Deck now also reports ACH 87 while completing recurring MRAC
exchanges, so ACH itself does not separate the Deck from the failing desktop.
The native ACH 77 route also fails before MRAC on the exact installed Valve
Proton `11.0-100`, so switching from GE-Proton11-6 alone is insufficient.
The game inherits `AC_LAUNCHMODE=0x00009609` in every measured case. ACH is
therefore a separate opaque MGL controller field, not the decimal rendering of
`AC_LAUNCHMODE`. MGL job-mode IDs are not a direct mapping either: job mode 3
has accompanied 77, 120, and 128. Proton, `SteamDeck=1`, and channel 47 do not
directly select ACH.

[`run-wrf-ach-boundary-trace.sh`](run-wrf-ach-boundary-trace.sh) records a
payload-free native or Steam launch boundary:

```bash
./Steam/source/run-wrf-ach-boundary-trace.sh native
./Steam/source/run-wrf-ach-boundary-trace.sh steam
```

The native command is autonomous: [`wrf-native-autoplay.py`](wrf-native-autoplay.py)
visually confirms the Frontiers Play button, retries a missed direct X11 event,
waits for a stable window owned by the live game process, and refocuses it for
delayed intro-skip Space events. If X11 window capture is unavailable, it uses
the launcher's measured window geometry to select Frontiers and Play, then still
requires a new window owned by the game before sending input.

Both commands also run the hash-gated
[`run-wrf-launchmode-service-probe.sh`](run-wrf-launchmode-service-probe.sh).
It records only HTTP status, declared/read byte counts, and the hexadecimal
parse result at the four `aclaunchapi64.dll` decision boundaries. It does not
capture the response body. Current clean controls produced the same result:

| Route | ACH | Service decision | Game inheritance |
| --- | ---: | --- | --- |
| Native build 133 | 77 | HTTP 200, 10/10 bytes, valid `0x00009609` | `0x00009609` |
| Steam build 132 | 87 | HTTP 200, 10/10 bytes, valid `0x00009609` | `0x00009609` |

The fresh autonomous native control reached AC Online and then received
backend close 1006 after `8.131 s`; no Gate0018/MRAC request appeared. The
identical valid service selection across ACH 77 and 87 proves that
`AC_LAUNCHMODE` is not the missing ACH selector in these controls.

The separate `GE-Proton11-6-WRF-ACHProbe` candidate adds one `wrfach` Wine
debug channel. Its KernelBase layer records exact-name reads/writes, while its
64-bit ntdll layer records exact-name operations and process-start inheritance
of `AC_LAUNCHMODE`. Events include the process and caller module/RVA where
available, without tracing the general environment or network payloads:

```bash
./Steam/source/build-ge-proton-11-ach-probe.sh
```

After selecting that tool, use the normal options with `+wrfach` added:

```bash
PROTON_LOG=1 WINEDEBUG="warn+module,+wrfach" WRF_PREFER_ACCLIENT_BASE=1 WINEDLLOVERRIDES="GCLay.dll=d;GCLay64.dll=d" SteamDeck=1 %command%
```

### GE-Proton11-6 MRAC compatibility candidate

The local `GE-Proton11-6-WRF-MRAC` candidate layers the measured compatibility
surfaces onto the ACH probe runtime:

- exact-name `acclient64.dll` preferred-base loading;
- stale TLS-directory pointer repair if preferred-base loading falls back;
- `SystemDmaGuardPolicyInformation` class 202 support;
- host-backed ACPI table reads and TPM EK public-key exposure through
  `PCP_EKPUB`;
- the existing `wrfach` launch-mode probes.

The 2026-08-29 build-133 run verified each relevant active boundary. Wine mapped
`acclient64.dll` at `0x180000000`, class 202 no longer returned
`STATUS_INVALID_INFO_CLASS`, the protected client read the real 283-byte TPM EK,
and ACH 77 inherited `AC_LAUNCHMODE=0x00009609`. No MRAC RPC was generated: the
backend closed 8.500 seconds after AC Online and client-request sends began
failing about 70 seconds after AC Online. A same-build GE-Proton10-34
preferred-base control also produced no MRAC RPC, correcting the older model:
preferred-base loading restored requests for the older client, but is not
sufficient for the current MRAC client/build.

The retained ACH inventory is current 77, 87, 120, and 128 plus historical
118. Testing 118 on the current build requires a matching official launcher
job, signed route, supported Deck route, or developer cohort; overriding
`AC_LAUNCHMODE` does not select ACH and would create a synthetic comparison.
See
[`documentation/07-ach-launch-mode-analysis.md`](../../documentation/07-ach-launch-mode-analysis.md)
for the static evidence and test matrix.
