---
type: "diagnostic"
date: "2026-08-29T20:33:27.732970+00:00"
question: "Can you go back through the history and figure out what the unmet request prerequisite is?"
contributor: "graphify"
outcome: "corrected"
correction: "Do not describe a specific local API or hardware field as the known unmet prerequisite. The clean exact-runtime Steam ACH 87 A/B is itself still missing; until it is run, only a pre-Gate session-arming condition is supported as an inference."
source_nodes: ["Current Gate0018 Failure Boundary", "Successful Steam Deck Behavioral Comparison", "Gate0018 Zero-Byte Request Result", "FMracServiceWs Client Request Bridge", "ACH Route and Result Matrix", "Backend Validation or Session Lease"]
---

# Q: Can you go back through the history and figure out what the unmet request prerequisite is?

## Answer

Expanded query: history request mrac client gate bridge launch route steam deck authentication validation. The history does not yet prove a missing local Wine API. It proves that current request generation requires an arming condition established before Gate0018: the healthy same-build Deck ACH 87 session queues a 4096-byte request, while current desktop paths return zero. ACH, AC_LAUNCHMODE, RPC ID 47, anti-cheat binary hashes, GCLay disablement, SteamDeck=1, preferred-base loading, TLS relocation, DMA Guard class 202, host ACPI, TPM EK, SteamOS identity, CPU topology, and GC_PIPE_NAME availability are not sufficient. The strongest technical inference is a supported-platform anti-cheat session authorization/command/seed, likely associated with the separate acclient TLS handshake or backend policy. However the experimental prerequisite for making that inference decisive is still unmet: no completed signed-in official Steam ACH 87 run on the exact Valve Proton 11.0-100 used by the Deck exists. Desktop Steam was tested with GE-Proton11-6; exact Valve Proton was tested only through native ACH 77. Run the exact matched Steam control before another Wine patch.

## Outcome

- Signal: corrected
- Correction: Do not describe a specific local API or hardware field as the known unmet prerequisite. The clean exact-runtime Steam ACH 87 A/B is itself still missing; until it is run, only a pre-Gate session-arming condition is supported as an inference.

## Source Nodes

- Current Gate0018 Failure Boundary
- Successful Steam Deck Behavioral Comparison
- Gate0018 Zero-Byte Request Result
- FMracServiceWs Client Request Bridge
- ACH Route and Result Matrix
- Backend Validation or Session Lease