---
type: "query"
date: "2026-08-29T15:20:32.697870+00:00"
question: "The latest capture has started. Deck ACH is actually 87 https://collect.odb-tech.com/v1/runs/967e02fa-1132-48b7-8fcd-a8a8b0e482c0"
contributor: "graphify"
outcome: "corrected"
correction: "The current supported Steam Deck selects ACH 87, not ACH 118. Compare same-build Deck and desktop at the first protected MRAC request boundary; do not require a fixed RPC ID 47."
source_nodes: ["ACH Route and Result Matrix", "Successful Steam Deck Behavioral Comparison", "Backend Validation Investigation", "Gate0018 Zero-Byte Request Result", "FMracServiceWs Client Request Bridge"]
---

# Q: The latest capture has started. Deck ACH is actually 87 https://collect.odb-tech.com/v1/runs/967e02fa-1132-48b7-8fcd-a8a8b0e482c0

## Answer

Expanded from original observation via graph vocabulary: [ach, deck, steam, launch, mode, gate, mrac, service, route, rpc, backend, validation]. Current Deck run 967e02fa-1132-48b7-8fcd-a8a8b0e482c0 directly reports ACH 87 on game build 24860441 and Valve Proton 11.0-100. Its anti-cheat DLL hashes match the current desktop Steam copies. It completed normally after about 15m14s with 8/8 MRAC calls, 31/31 session pings, no collector gap, and no backend close. Therefore ACH 87 does not distinguish supported Deck success from desktop failure, ACH 118 is historical rather than the current target, and RPC ID 47 is not a fixed MRAC channel: the first MRAC call used ID 44 while ID 47 was a maintenance request. A native Valve Proton 11.0-100 control still emitted no MRAC request and closed 8.648s after AC Online, so the remaining clean A/B is signed-in desktop Steam ACH 87 on Valve Proton 11.0-100.

## Outcome

- Signal: corrected
- Correction: The current supported Steam Deck selects ACH 87, not ACH 118. Compare same-build Deck and desktop at the first protected MRAC request boundary; do not require a fixed RPC ID 47.

## Source Nodes

- ACH Route and Result Matrix
- Successful Steam Deck Behavioral Comparison
- Backend Validation Investigation
- Gate0018 Zero-Byte Request Result
- FMracServiceWs Client Request Bridge