# War Robots: Frontiers Linux compatibility tools

Compatibility setup for War Robots: Frontiers on Linux and Steam Deck.

| Distribution | Status |
| --- | --- |
| Steam | Launches with the bundled custom Proton build; later backend disconnect under investigation |
| MY.GAMES launcher | ACH 118 proof achieved with MY.GAMES auth; later backend disconnect under investigation |

The complete investigation record, route matrix, evidence rules, and SteamOS VM
test plan are in [documentation/](documentation/README.md).

## Remote Steam Deck comparison

The comprehensive remote collector, including tightly scoped MRAC
request/response capture, GHCR/Portainer deployment, SteamOS install, signed
updates, and one-command uninstall, is documented in
[Collector/README.md](Collector/README.md).

## Steam installation

### Part 1: Install the compatibility tool

1. Install the Steam version of War Robots: Frontiers.
2. From this repository, run:

   ```bash
   ./Steam/setup.sh
   ```

If Steam is open, the installer waits without changing files until Steam has fully exited. Press `Ctrl+C` to cancel instead.

For a custom Steam location, pass its compatibility-tools directory:

```bash
./Steam/setup.sh --target /path/to/compatibilitytools.d
```

### Part 2: Configure and launch the game

1. Start Steam and open **War Robots: Frontiers → Properties → Compatibility**.
2. Enable **Force the use of a specific Steam Play compatibility tool**.
3. Select **GE-Proton10-34-WRF-TLS**.
4. Under **Properties → General → Launch Options**, enter exactly:

   ```bash
   WINEDLLOVERRIDES="GCLay.dll=d;GCLay64.dll=d" SteamDeck=1 %command%
   ```

5. Launch the game normally through Steam.

The bundled build contains the Wine TLS relocation compatibility fix required by the Steam anti-cheat path. The route reports `ACH=118`, starts the anti-cheat client in user mode, and reaches the hangar. Current controlled tests then receive a remote backend close after roughly 64 seconds; this runtime should therefore be treated as diagnostic, not a complete fix.

### Troubleshooting

- Restart Steam after installing the compatibility tool; Steam discovers new tools only during startup.
- Confirm the selected tool is `GE-Proton10-34-WRF-TLS`, not another GE-Proton release.
- Keep the launch options exactly as shown. Disabling both GCLay DLLs is required.
- The installer refuses to overwrite an existing copy. Remove the old `GE-Proton10-34-WRF-TLS` directory manually before reinstalling.

## MY.GAMES launcher installation

### Part 1: Install the launcher compatibility runtime

Work in progress. A tested runtime and setup command will be added here.

### Part 2: Configure and launch the game

Work in progress. A laboratory launcher-job test has reached `ACH=118` while retaining the MY.GAMES account, but it encounters the same later remote disconnect as the normal Steam control. Do not reuse the Steam launch options for the native launcher yet.

## Source and licensing

This repository includes a modified GE-Proton runtime. Base source, exact revisions, local patch, and build provenance are documented in [Steam/source/README.md](Steam/source/README.md). Component licenses remain included inside the runtime.
