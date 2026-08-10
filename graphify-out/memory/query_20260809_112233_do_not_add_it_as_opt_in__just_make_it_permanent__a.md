---
type: "query"
date: "2026-08-09T11:22:33.202744+00:00"
question: "Do not add it as opt-in. Just make it permanent. Add all possible extensive collector features and enable them all."
contributor: "graphify"
outcome: "useful"
source_nodes: ["event", "parseGameLine", "validateEvent", "ingest", "landingTemplate"]
---

# Q: Do not add it as opt-in. Just make it permanent. Add all possible extensive collector features and enable them all.

## Answer

Implemented a permanent complete diagnostic profile. The collector now extracts only complete Base64 values from FMracServiceWs::ClientRequest call/response JSON, validates strict Base64, decoded size, SHA-256, method and state, stores them in protected JSONL, reports payload counts in the admin dashboard, and batches uploads below the 1 MiB limit. Existing normalized RPC, transport, lifecycle, runtime, process, environment-presence, DLL hash, liveness, and structured probe paths remain always enabled. The UI and installer disclose sensitive MRAC attestation capture while excluding source logs, credentials, tokens, command lines, packet captures, memory dumps, and unrelated RPC bodies. Protobuf logging is VeryVerbose and signed auto-updates now restart reliably.

## Outcome

- Signal: useful

## Source Nodes

- event
- parseGameLine
- validateEvent
- ingest
- landingTemplate