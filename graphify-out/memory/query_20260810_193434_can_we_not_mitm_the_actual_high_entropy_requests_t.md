---
type: "query"
date: "2026-08-10T19:34:34.429636+00:00"
question: "Can we not MitM the actual high-entropy requests to properly see what is going on inside of WSS?"
contributor: "graphify"
outcome: "useful"
source_nodes: ["FMracServiceWs Client Request Bridge", "Payload-Free Backend RPC Timeline", "Successful Steam Deck Behavioral Comparison", "Backend Validation Investigation"]
---

# Q: Can we not MitM the actual high-entropy requests to properly see what is going on inside of WSS?

## Answer

Expanded from graph vocab: [deck, mrac, request, response, payload, bridge, rpc, transport, backend, validation, capture, successful]. A WSS MITM would reveal the same application-layer RPC/Base64 body already captured at FMracServiceWs::ClientRequest. The 4096-byte body remains high entropy because protection is inside the MRAC envelope before WSS/TLS, so TLS interception cannot decode attestation. Useful next boundaries are a vendor debug build/server-side validation decision, or a sanctioned hook before the MRAC envelope is constructed; hooking send/SSL/WebSocket only duplicates existing evidence. Also re-baseline because active local anti-cheat hashes now differ from the successful Deck capture despite the same Steam build ID.

## Outcome

- Signal: useful

## Source Nodes

- FMracServiceWs Client Request Bridge
- Payload-Free Backend RPC Timeline
- Successful Steam Deck Behavioral Comparison
- Backend Validation Investigation