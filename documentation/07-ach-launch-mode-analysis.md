# ACH and `AC_LAUNCHMODE`

## Corrected model

`ACH` and `AC_LAUNCHMODE` are distinct values.

On 2026-08-29, narrow 64-bit Wine probes measured current ACH 77, 87, 120, and
128 routes. Every game process inherited the same value:

```text
AC_LAUNCHMODE=0x00009609
```

That value exactly matches `<defaultLaunchMode>0x9609</defaultLaunchMode>` in
the signed anti-cheat config. Consequently, converting 77, 87, 118, 120, or 128
from decimal to hexadecimal does **not** reveal `AC_LAUNCHMODE`. Earlier notes
that treated those conversions as observed mappings were incorrect.

MGL contains the strings `AnticheatController`, `AntiCheatUtil`, `ACH=`,
`CreateAntiCheatInstaller`, and `CreateAntiCheatLauncher`. This places the ACH
log field in the launcher's anti-cheat-controller path, but does not establish
its vendor-defined semantics. Current evidence only supports treating ACH as an
opaque MGL launch/controller selection associated with a launcher contract.
MGL's job-mode ID is not a direct ACH mapping either: job mode 3 accompanied
77, 120, and 128 in different measured launcher contracts.

The retained observed ACH inventory is:

| ACH | Reproduced context | Game-facing `AC_LAUNCHMODE` |
| ---: | --- | --- |
| 77 | Current native build 133; MGL game job modes 2 and 3 observed | `0x00009609`, measured |
| 87 | Current Steam build 132; MGL job mode 5 | `0x00009609`, measured |
| 118 | Historical official Steam/Deck-compatible route; laboratory hybrid explicitly rewrote MGL-selected 120 to 118 | Unknown; probe did not exist |
| 120 | Current build 133, native auth plus Steam launch contract; MGL normalized channel 47 to 31, job mode 3 | `0x00009609`, measured |
| 128 | Current build 133, native auth plus legacy Steam-bootstrap contract; job mode 3 | `0x00009609`, measured |

No other genuine ACH value was found in the retained launcher logs. Broad text
searches produce false positives from source documentation and words such as
`DETACH`; those are not launcher records.

Windows API stubs observed during 77 and 87 runs do not prove that either ACH
value means "Windows attestation." The fact that four different ACH values
deliver the same game-facing launch mode weakens that interpretation: any
behavioral difference may be upstream MGL handling, other launch metadata,
protected-code inputs, or unrelated Wine gaps.

## Static `aclaunchapi64.dll` evidence

The current native and Steam `aclaunchapi64.dll` files have different
whole-file hashes, but their loadable `.text`, `.rdata`, `.data`, `.pdata`,
`.rsrc`, and `.reloc` sections are byte-identical. The differences are PE
checksum/Authenticode data, not different launch-mode code.

The common executable image has this data flow:

1. It initializes its mode field from the configured default, currently
   `0x9609`.
2. It reads the `SteamDeck` environment flag.
3. It requests `/launchmode?user=...` from the configured anti-cheat host.
4. Only an HTTP 200 response containing a valid, non-empty hexadecimal value
   that is fully consumed by the parser overwrites the default.
5. It formats the resulting field as `0x%08x` and supplies it to the launch path
   as `AC_LAUNCHMODE`.

An observation-only decision probe now resolves that ambiguity for the current
native ACH 77 and Steam ACH 87 routes. Both received HTTP 200, reported and
read exactly 10 response bytes, fully consumed a valid hexadecimal value, and
selected `0x00009609`. The response body was not captured. Therefore this
value is not merely a local fallback in those two controls: the service
actively returned a valid value equal to the configured default.

The result narrows the model further. Current ACH 77 and 87 differ even though
the launch-mode service selects the same value for both. The service-selected
mode does not explain their ACH selection and changing it locally would create
a mismatched policy experiment. ACH 118's historical service response remains
unmeasured.

The relevant measured image addresses are:

- initial mode-field copy near `0x18001e465`;
- `/launchmode?user=` request construction near `0x18001e5e9`;
- HTTP 200 handling near `0x18001e708`;
- radix-16 parsing near `0x18001e7c2`;
- validated overwrite at `0x18001e7f4`;
- `0x%08x` formatting near `0x18001f898`; and
- `SetEnvironmentVariableW(L"AC_LAUNCHMODE", ...)` near `0x18001faf4`.

The signed config is identical between the measured routes:

```xml
<defaultLaunchMode>0x9609</defaultLaunchMode>
<host>wrf.anticheat.my.games</host>
<port>443</port>
<secure>true</secure>
```

This launch-mode service path is therefore real, but it is not evidence that
the separately logged ACH number is the service response.

## Dynamic boundary trace

Run the payload-safe tracer against either route:

```bash
./Steam/source/run-wrf-ach-boundary-trace.sh steam
./Steam/source/run-wrf-ach-boundary-trace.sh native
```

It records the selected compatibility tool, launcher/config hashes, loadable
PE-section fingerprints, connection syscalls from the launcher leader, and the
observed ACH. It reads the exact allowlisted `wrfach` inheritance event when
Linux `/proc` cannot see Wine's Windows environment block. On the native route
it also visually confirms Play, launches the game, identifies the real game
window by its live process, and sends delayed Space events to skip the intro.
Raw metadata is mode `0600`, remains under the private
`ach-boundary-traces` directory, and must not be uploaded to the collector.
HTTP/TLS bodies, command lines, credentials, and the general environment are
excluded.

The runner also starts
[`run-wrf-launchmode-service-probe.sh`](../Steam/source/run-wrf-launchmode-service-probe.sh).
That hash-gated GDB probe observes four post-call decisions inside the current
native and Steam `aclaunchapi64.dll` images: HTTP status, declared response
length, bytes read, and hexadecimal parse result. It never reads or writes the
response body. Its standalone form must be started before Play when a narrower
cycle is desired.

## GE-Proton11-6 game-boundary probe

[`kernelbase-wrf-ach-launchmode-probe-ge11.patch`](../Steam/source/kernelbase-wrf-ach-launchmode-probe-ge11.patch)
records exact-name KernelBase `GetEnvironmentVariableW` and
`SetEnvironmentVariableW` operations.
[`ntdll-wrf-ach-launchmode-probe-ge11.patch`](../Steam/source/ntdll-wrf-ach-launchmode-probe-ge11.patch)
records exact-name ntdll query/set operations and the `AC_LAUNCHMODE` value
inherited at process startup. Events contain the process, operation, result,
caller module/RVA where available, and the single allowlisted value. They do
not record the general environment or network payloads.

Build the separate runtime:

```bash
./Steam/source/build-ge-proton-11-ach-probe.sh
```

Then select `GE-Proton11-6-WRF-ACHProbe` and use:

```bash
PROTON_LOG=1 WINEDEBUG="warn+module,+wrfach" WRF_PREFER_ACCLIENT_BASE=1 WINEDLLOVERRIDES="GCLay.dll=d;GCLay64.dll=d" SteamDeck=1 %command%
```

Inspect only probe events with:

```bash
rg 'wrfach:' /home/owendb/Games/wf-frontiers/steam-1491000.log
```

The configured GE-Proton11-6 build is x86_64-only, so the probe establishes the
64-bit game boundary, not reads performed inside 32-bit MGL. In the measured
77, 87, 120, and 128 runs, ntdll recorded startup inheritance of
`0x00009609`; no later 64-bit query by the game was observed. Protected code
may read the process environment block directly, or the value may be consumed
before that code path. Startup inheritance itself is definitive.

The current build-133 120 route reached AC Online and then closed after
`8.725 s`; the current build-133 128 route closed after `8.090 s`. Neither
emitted a Gate0018/MRAC request. This differs from an older ACH 120 build that
did complete an MRAC exchange, so ACH alone is not a stable behavioral profile
across builds and launcher contracts.

## Testing every observed ACH value

Values 77, 87, 120, and 128 have now been reproduced through distinct current
controlled contracts executed by the official MGL binary. Value 118 remains
historical-only. No supported local switch was found that selects an ACH value,
and setting
`AC_LAUNCHMODE` does not select ACH—the current runs prove the fields are
separate.

A faithful current 118 comparison therefore needs an official launcher job,
signed route, supported Deck route, or developer test cohort that makes MGL
select it. Locally injecting 118 or replacing `AC_LAUNCHMODE` would test a
synthetic client/policy mismatch, not the original route. Channel 47,
`SteamDeck=1`, and the selected Proton build also do not directly select ACH.

The historical official Steam ACH 118 job and the current official Steam ACH
87 job both carry Steam app id 1491000 and `-nosplash -LaunchFromSteam`; both
use channel 47 configuration. The current native and Steam MGL executables are
also byte-identical. Those local fields are therefore insufficient to recover
118. The remaining selector is upstream of the game-facing job—launcher/game
revision, authorization context, backend policy or cohort, or another signed
input. The old MY.GAMES-account hybrid did not discover a local selector: its
laboratory pipe rewrite explicitly substituted ACH 118 and the Steam launch
contract after MGL had selected 120.

The next valid all-mode matrix is:

1. retain the same game files, account, collector, and probe runtime;
2. have the sanctioned launcher/backend select one ACH value per run;
3. record MGL job mode, ACH, inherited `AC_LAUNCHMODE`, AC Online time,
   Gate0018 request length, MRAC RPC id 47 timing, and backend close time; and
4. compare request generation only after confirming the selected route.

The six retained remote collector runs predate ACH capture and report
`ach_observed=false`. Updated collector clients capture ACH on future runs, so
new Deck/current-route captures can populate this matrix without inference.
