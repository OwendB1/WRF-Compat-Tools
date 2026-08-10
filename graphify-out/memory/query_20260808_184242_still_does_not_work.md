---
type: "query"
date: "2026-08-08T18:42:42.165339+00:00"
question: "Still does not work"
contributor: "graphify"
outcome: "useful"
source_nodes: ["Wine Ntdll Stale TLS Pointer Rebase Patch", "Gate0018 Zero-Byte Request Result", "FMracServiceWs Client Request Bridge"]
---

# Q: Still does not work

## Answer

Expanded from original query via graph vocab: [proton, wine, tls, gate, mrac, runtime]. The new run used the patched Valve runtime and rebased four TLS directory pointers plus two callbacks, but immediately repeated the same Wine exception-stack overflow before AC Online. This moves the failure from relocation bookkeeping to the protected exception/unwind path; collect +seh,+unwind,+loaddll once without the SteamOS wrapper to separate Wine behavior from the spoof wrapper.

## Outcome

- Signal: useful

## Source Nodes

- Wine Ntdll Stale TLS Pointer Rebase Patch
- Gate0018 Zero-Byte Request Result
- FMracServiceWs Client Request Bridge