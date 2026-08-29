---
type: "investigation"
date: "2026-08-29T21:33:32.223034+00:00"
question: "We have gotten prior approval. Lets actually try to analyze the MRAC call before the encode into high-entropy happens so we can know what kind of data is sent towards the backend to attest/satisfy possibly steam deck compatibility."
contributor: "graphify"
outcome: "useful"
source_nodes: ["Backend Validation Investigation", "FMracServiceWs Client Request Bridge", "Gate0018 Zero-Byte Request Result", "Compatibility Research Scope Boundary", "Payload-Free Evidence Handling", "Current Gate0018 Failure Boundary"]
---

# Q: We have gotten prior approval. Lets actually try to analyze the MRAC call before the encode into high-entropy happens so we can know what kind of data is sent towards the backend to attest/satisfy possibly steam deck compatibility.

## Answer

A same-process observation-only trace proved the final 4096-byte MRAC request is bytes 4..4099 of a transient anonymous 8192-byte allocation. The allocation is initialized with a private state word, receives a four-byte prefix, becomes high entropy between protected writer RVAs 0x4e405c4 and 0x26050c, returns its state word to 1, and is handed off 7.90 ms later from caller RVA 0x27ac69b. Exact raw artifacts remain private outside the repository. No readable Deck, DMI, ACH, JSON, or protobuf-like fields were observed before the captured encoding transition.

## Outcome

- Signal: useful

## Source Nodes

- Backend Validation Investigation
- FMracServiceWs Client Request Bridge
- Gate0018 Zero-Byte Request Result
- Compatibility Research Scope Boundary
- Payload-Free Evidence Handling
- Current Gate0018 Failure Boundary