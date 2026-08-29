---
type: "query"
date: "2026-08-29T23:15:29.481929+00:00"
question: "DMA Guard class 202 fails with 0xc0000003 and PCP_EKPUB fails with 0x80090027; these are the most noteworthy things to fix."
contributor: "graphify"
outcome: "useful"
source_nodes: ["Backend Validation Investigation", "Compatibility Research Scope Boundary", "Payload-Free Evidence Handling", "Successful Steam Deck Behavioral Comparison"]
---

# Q: DMA Guard class 202 fails with 0xc0000003 and PCP_EKPUB fails with 0x80090027; these are the most noteworthy things to fix.

## Answer

Expanded via graph vocabulary: [Backend Validation Investigation, Compatibility Research Scope Boundary, Payload-Free Evidence Handling, Current Gate0018 Failure Boundary, Gate0018 Zero-Byte Request Result, Successful Steam Deck Behavioral Comparison]. Both surfaces are noteworthy controlled variables but already have working local implementations. The GE-Proton11-6-WRF-MRAC run returned class-202 success and exposed the authentic 283-byte host EK, yet still closed after 8.5 seconds without MRAC on ACH 77; the older GE10 host-security run also still closed. Conversely, the exact Valve Proton 11.0-100 official Steam ACH 87 control generated a 4096-byte MRAC request despite both failures. Therefore neither failure blocks request construction. The unresolved test is a status-only measurement of those calls on the healthy Deck, then an exact official Steam ACH 87 A/B; do not infer that forcing success is a fix before that comparison.

## Outcome

- Signal: useful

## Source Nodes

- Backend Validation Investigation
- Compatibility Research Scope Boundary
- Payload-Free Evidence Handling
- Successful Steam Deck Behavioral Comparison