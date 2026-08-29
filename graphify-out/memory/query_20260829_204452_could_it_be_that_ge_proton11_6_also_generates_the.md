---
type: "query"
date: "2026-08-29T20:44:52.958246+00:00"
question: "Could it be that GE-Proton11-6 also generates the payload but we did not see it because we did not have the correct verbose RPC logging?"
contributor: "graphify"
outcome: "corrected"
correction: "Non-verbose game logs cannot establish that GE-Proton11-6 failed to generate MRAC. Its 16.793-second timeline nearly matches the Valve MRAC-producing control; rerun GE11 with identical verbose RPC logging."
source_nodes: ["Current Gate0018 Failure Boundary", "FMracServiceWs Client Request Bridge", "Payload-Free Backend RPC Timeline", "GE-Proton10-34-WRF-TLS Runtime Provenance", "Backend Validation Investigation"]
---

# Q: Could it be that GE-Proton11-6 also generates the payload but we did not see it because we did not have the correct verbose RPC logging?

## Answer

Expanded from graph vocabulary: [proton, valve, request, mrac, gate, client, bridge, runtime, steam, payload, response, current]. Yes, and the timing makes it likely. The earlier GE-Proton11-6 official Steam run had zero GLogBackendRpcWsCalls/FMrac verbose lines and closed 16.793 seconds after connection. The first stock Valve Proton run had the same non-verbose observability gap and closed at 16.766 seconds, falsely appearing request-free. Enabling GLogBackendRpcWsCalls and MRAC on a repeat exposed a 4096-byte-equivalent MRAC call at 5.700 seconds, response at 14.127 seconds, and close at 17.044 seconds. Therefore ordinary-log absence is not evidence of absent MRAC, and all current GE11 no-request conclusions based only on those logs must be withdrawn. A GE11-6 official Steam ACH 87 repeat with identical verbose categories is required.

## Outcome

- Signal: corrected
- Correction: Non-verbose game logs cannot establish that GE-Proton11-6 failed to generate MRAC. Its 16.793-second timeline nearly matches the Valve MRAC-producing control; rerun GE11 with identical verbose RPC logging.

## Source Nodes

- Current Gate0018 Failure Boundary
- FMracServiceWs Client Request Bridge
- Payload-Free Backend RPC Timeline
- GE-Proton10-34-WRF-TLS Runtime Provenance
- Backend Validation Investigation