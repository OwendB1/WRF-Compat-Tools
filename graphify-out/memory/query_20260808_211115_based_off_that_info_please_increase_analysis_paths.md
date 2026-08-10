---
type: "query"
date: "2026-08-08T21:11:15.031077+00:00"
question: "Based off that info please increase analysis paths and then run the game again. If you have patch ideas please execute them."
contributor: "graphify"
outcome: "corrected"
correction: "For the preferred-base Valve Proton runtime, Gate/request generation succeeds; backend rejection occurs 2.967 seconds after a completed 697-byte MRAC response."
source_nodes: ["Gate0018 Zero-Byte Request Result", "FMracServiceWs Client Request Bridge", "Backend Validation or Session Lease", "Successful Steam Deck Behavioral Comparison", "Wine Ntdll Stale TLS Pointer Rebase Patch"]
---

# Q: Based off that info please increase analysis paths and then run the game again. If you have patch ideas please execute them.

## Answer

Expanded from graph vocab: [backend, client, deck, gate, mrac, proton, request, response, steam, tls, validation]. Added payload-free collector analysis for ACClient send failures and raised anti-cheat hashing from 64 MiB to 128 MiB. The preferred-base Proton rerun reached ACH 118 login and AC Online. Relative to AC Online, the first attempt and FMracServiceWs::ClientRequest call occurred at +4.897s/+4.898s; RPC id 47 returned after 8.551s and ACClient received a 697-byte response at +13.449s. Backend close 1006 followed at +16.416s, 2.967s after response. There were no RPC errors, no access violation, and ordinary RPCs completed after MRAC. This corrects the current boundary: preferred-base mapping fixes loader and Gate/request generation, while the remaining failure is post-exchange validation or state transition. No speculative Proton behavior patch was added without a measured runtime incompatibility.

## Outcome

- Signal: corrected
- Correction: For the preferred-base Valve Proton runtime, Gate/request generation succeeds; backend rejection occurs 2.967 seconds after a completed 697-byte MRAC response.

## Source Nodes

- Gate0018 Zero-Byte Request Result
- FMracServiceWs Client Request Bridge
- Backend Validation or Session Lease
- Successful Steam Deck Behavioral Comparison
- Wine Ntdll Stale TLS Pointer Rebase Patch