# Evidence and security rules

## Evidence locations

Primary local reports predating this documentation:

- `<private-workspace>/WRF-Linux-diagnostics-summary.md`
- `<private-workspace>/ProtonDB-report-draft.md`

Important sanitized trace indexes:

- Native MY.GAMES/Steam-target capture:
  `launcher-research/traces/mygames/20260807-094505/records/index.sanitized.json`
- ACH 118 MY.GAMES hybrid:
  `launcher-research/traces/hybrid/20260807-101356/records/index.sanitized.json`

Normal Steam control cycles:

- `steam-cycles/20260807-105134`
- `steam-cycles/20260807-105400`
- `steam-cycles/20260807-105641`
- `steam-cycles/20260807-113617` (verbose RPC metadata control)
- `steam-cycles/20260807-115517` (verbose RPC metadata reproduction)

Payload-free RPC timeline from the last control:

- `steam-cycles/20260807-113617/backend-rpc-timeline.tsv`
- `steam-cycles/20260807-115517/backend-rpc-timeline.tsv`

Private network diagnostic captures:

- `launcher-research/traces/steam-control-diagnostic/steam-1491000-winsock-schannel-20260807-105400.raw.log`
- `launcher-research/traces/steam-control-diagnostic/steam-1491000-secur32-20260807-105641.raw.log`
- `launcher-research/traces/steam-control-diagnostic/steam-1491000-file-sync-20260807-124724.raw.log`
- `launcher-research/traces/steam-control-diagnostic/steam-relay-20260807-130525/steam-1491000.log`

All relative paths above are under `<private-workspace>/`.

## Handling rules

- Raw launcher, pipe, Wine, Schannel, and network traces remain local with mode
  `0600`.
- Never commit raw traces to this repository.
- Before sharing, redact account/user IDs, tokens, nicknames, signed URLs,
  trace/pipe IDs, machine IDs, local paths, and TPM blobs.
- Treat a file named `sanitized` as untrusted until manually checked; automated
  redaction can miss an unknown field.
- Publish timestamps only when they do not correlate to a private session.
- Record network payload sizes and status transitions, not proprietary bodies,
  except for the collector's narrowly scoped MRAC ClientRequest comparison.
- Treat verbose `GLogBackendRpcWsCalls` output as secret: it includes full RPC
  bodies and authentication material. Keep source logs mode `0600`; never
  upload whole lines. The collector may extract only the complete Base64 value
  inside `FMracServiceWs::ClientRequest` calls and responses, with a verified
  decoded length and SHA-256. All authentication and unrelated RPC bodies stay
  local.
- Treat collected MRAC blobs as sensitive attestation material: restrict them
  to the administrator-protected run API, retain them only for the configured
  study period, and never publish, modify, or replay them.
- Do not collect or retain private key material.

## Interpretation discipline

Use these labels in reports:

- **Observed:** directly present in logs, captures, hashes, or process state.
- **Inferred:** best explanation connecting observations, with alternatives
  still possible.
- **Unknown:** requires vendor knowledge or a new controlled experiment.

Examples:

- Observed: both account routes reach ACH 118 and receive a remote TCP EOF near
  64 seconds.
- Inferred: this boundary is a later platform/session validation check.
- Unknown: which exact client property or server policy causes rejection.

## Scope boundary

Allowed work:

- implement missing Wine/Windows compatibility behavior;
- use authentic public host information;
- compare non-unique DMI model strings, PCI IDs, CPU topology, firmware-table
  availability/sizes, and presence-only identity/API signals;
- compare official routes and binaries;
- passively observe launcher-generated local messages;
- compare authentic MRAC request/response blobs captured from controlled runs
  without decoding, modifying, or replaying them;
- replay a launcher-generated job while keeping official auth in control.

Out of scope:

- forging Deck identity, TPM state, firmware tables, or attestation results;
- bypassing or disabling anti-cheat enforcement;
- modifying encrypted/proprietary anti-cheat payloads;
- capturing credentials or unrelated private authentication responses, or
  replaying any captured response;
- impersonating a supported device to a remote service.
