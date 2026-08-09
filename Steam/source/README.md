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

## Valve Proton 10.0 A/B candidate

The successful Steam Deck capture identified Steam compatibility version
`10.1000-105`. The matching installed Valve runtime reports
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
