---
type: "query"
date: "2026-08-08T20:41:43.506424+00:00"
question: "We have a new remote capture you can inspect."
contributor: "graphify"
outcome: "useful"
source_nodes: ["Successful Steam Deck Behavioral Comparison", "FMracServiceWs Client Request Bridge", "Gate0018 Zero-Byte Request Result", "Backend Validation or Session Lease"]
---

# Q: We have a new remote capture you can inspect.

## Answer

Expanded from original query via graph vocab: [backend, client, deck, gate, mrac, proton, request, response, steam, tls, validation]. The newest supported Steam Deck capture (build 24552611, Proton 10.1000-105) stayed healthy for 16m30s. AC came online, a client-request attempt appeared at +4.632s, FMracServiceWs::ClientRequest was called at +4.633s, and a 697-byte client response arrived at +4.832s. Nine real MRAC calls got nine responses, 33 pings got 33 responses, and there was no backend close. This reinforces the Backend Validation or Session Lease model: an early real MRAC call distinguishes the healthy Deck from the failing desktop Gate0018 path. The current desktop log lacks VeryVerbose categories, so it cannot yet prove whether a pre-close MRAC call was absent. The Deck aclauchapi64.dll hash also differs from both local copies, while acclient64.dll was skipped by the collector's 64 MiB hash limit.

## Outcome

- Signal: useful

## Source Nodes

- Successful Steam Deck Behavioral Comparison
- FMracServiceWs Client Request Bridge
- Gate0018 Zero-Byte Request Result
- Backend Validation or Session Lease