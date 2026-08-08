# Runtime source and provenance

`GE-Proton10-34-WRF-TLS` is based on GE-Proton10-34 with one WRF-specific Wine loader patch.

## Revisions

- GE-Proton source: [`GE-Proton10-34`](https://github.com/GloriousEggroll/proton-ge-custom/tree/GE-Proton10-34), commit `721dd76896b434fe3c1328ea533e0b25b4af04d5`
- Wine submodule: [ValveSoftware/wine](https://github.com/ValveSoftware/wine), commit `1729f00e17e879f98f9df1f2bca86bc5d21a65df`
- Local modification: [`ntdll-rebase-stale-tls-pointers.patch`](ntdll-rebase-stale-tls-pointers.patch)

The patch is applied from `patches/protonprep-valve-staging.sh` after the existing pending Wine hotfixes are selected and before the Wine build.

## Runtime verification

Key hashes from the packaged build:

```text
47e9fddf687792d041b2517e7ec24e7b8c0bc393d65b80261237a7c802fa893e  files/lib/wine/x86_64-unix/ntdll.so
d44cc662a943794089eb34965ca6f9fd5de5b0204af87748d2d8ce60e9ca1713  files/lib/wine/x86_64-windows/ntdll.dll
560ff791703000aaa5b593190ce0ca111d9b98ad6ff4426cbe49c225786b9535  proton
```

All upstream submodule revisions are recorded by the GE-Proton commit. Component license files are preserved in the packaged runtime.

## Valve Proton 10.0 A/B candidate

The successful Steam Deck capture identified Steam compatibility version
`10.1000-105`. The matching installed Valve runtime reports
`proton-10.0-4b`. Its exact source revisions are:

- Proton tag `proton-10.0-4b`, commit `e91ca2be0df2cef4c230cbbc0b86604d73a0bbf6`;
- Wine commit `b8fdff8e1f855b5276ec4ddca0f31b2792554322`; and
- the same local TLS relocation patch above.

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
