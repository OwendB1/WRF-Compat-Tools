---
type: "query"
date: "2026-08-08T19:13:48.999789+00:00"
question: "We got a fast shutdown now. ACH?"
contributor: "graphify"
outcome: "useful"
source_nodes: ["ACH 118 Steam Deck User-Mode Route", "Backend Validation Investigation", "Gate0018 Zero-Byte Request Result", "Wine Ntdll Stale TLS Pointer Rebase Patch"]
---

# Q: We got a fast shutdown now. ACH?

## Answer

Expanded from original query via graph vocab: [ach, anti, backend, client, gate, mrac, proton, tls, wine]. Verified launcher main.log selected ACH=118. New preferred-base Proton run moved Normaliz and forced acclient64 to 0x180000000, then ACClient reached Online and the hangar loaded. Backend WebSocket closed with code 1006 17.692 seconds after establishment; the game later performed a clean requested exit with status 0, not an access violation or stack overflow. Current non-verbose game log does not reveal whether MRAC was emitted or why the backend closed.

## Outcome

- Signal: useful

## Source Nodes

- ACH 118 Steam Deck User-Mode Route
- Backend Validation Investigation
- Gate0018 Zero-Byte Request Result
- Wine Ntdll Stale TLS Pointer Rebase Patch