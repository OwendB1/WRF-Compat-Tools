---
type: "query"
date: "2026-08-10T19:09:21.130167+00:00"
question: "Is the Steam Deck running Proton11? Preserve ACH 118 and RPC id 47, then apply the Steam Deck platform controls on top."
contributor: "graphify"
outcome: "corrected"
correction: "Deck comparison controls must be layered on the GE-Proton10-34 preferred-base ACH 118/RPC 47 baseline; GE-Proton11 ACH 77 runs are not valid comparisons."
source_nodes: ["ACH 118 Steam Deck User-Mode Route", "GE-Proton10-34-WRF-TLS Runtime Provenance", "FMracServiceWs Client Request Bridge"]
---

# Q: Is the Steam Deck running Proton11? Preserve ACH 118 and RPC id 47, then apply the Steam Deck platform controls on top.

## Answer

Expanded from repository vocabulary: [ach, valve, proton, runtime, route, mrac, gate, request, response, bridge, deck, successful]. Corrected the test baseline from GE-Proton11 to the GE-Proton10-34 preferred-base line. Built GE-Proton10-34-WRF-DeckCompare on HostSecurity, verified ACH 118 and a real FMracServiceWs::ClientRequest call/response on RPC id 47 at every cumulative stage, then added a process-local SteamOS 3.8.16 identity layer after Pressure Vessel. The final live game process saw steamdeck OS identity and 8 CPUs but still closed 2.667 seconds after the RPC response.

## Outcome

- Signal: corrected
- Correction: Deck comparison controls must be layered on the GE-Proton10-34 preferred-base ACH 118/RPC 47 baseline; GE-Proton11 ACH 77 runs are not valid comparisons.

## Source Nodes

- ACH 118 Steam Deck User-Mode Route
- GE-Proton10-34-WRF-TLS Runtime Provenance
- FMracServiceWs Client Request Bridge