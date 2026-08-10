---
type: "query"
date: "2026-08-09T16:02:10.069278+00:00"
question: "What can we do now then in terms of trying to get the non-deck version working?"
contributor: "graphify"
outcome: "useful"
source_nodes: ["Backend Validation Investigation", "Compatibility Implementation", "Wine Ntdll Stale TLS Pointer Rebase Patch", "SteamOS Userspace A/B Experiment", "Successful Steam Deck Behavioral Comparison"]
---

# Q: What can we do now then in terms of trying to get the non-deck version working?

## Answer

Expanded from original query via graph vocab: [proton, wine, tls, gate, mrac, backend, validation, response, runtime, deck, image, process]. The preferred-base patch already fixes loader and Gate0018 request generation on both exact Valve Proton 10.0-4b and GE-Proton10-34; another blind Proton rebase is low value. Next, run a local-only narrow trace for the three seconds after ACClient receives the first MRAC response, covering exception/unwind, TLS/thread lifecycle, waits/timers, crypto, device/firmware, registry and IPC calls. Then perform an official SteamOS userspace A/B on non-Deck hardware or a controlled VM/spare disk. If SteamOS succeeds, bisect Steam client session, Gamescope, kernel and service differences into a legitimate compatibility package. If SteamOS also fails while Deck works, the remaining boundary is hardware/platform validation or backend policy and cannot be repaired safely in Proton without vendor support. A run on an officially supported non-Deck SteamOS device is the cleanest decisive control. Do not spoof Deck identity, forge attestation, replay payloads, or suppress the server close.

## Outcome

- Signal: useful

## Source Nodes

- Backend Validation Investigation
- Compatibility Implementation
- Wine Ntdll Stale TLS Pointer Rebase Patch
- SteamOS Userspace A/B Experiment
- Successful Steam Deck Behavioral Comparison