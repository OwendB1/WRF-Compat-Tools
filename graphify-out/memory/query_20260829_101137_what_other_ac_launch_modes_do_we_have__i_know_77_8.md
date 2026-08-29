---
type: "compatibility-investigation"
date: "2026-08-29T10:11:37.282500+00:00"
question: "What other AC launch modes do we have? I know 77/87/118/120 at least. 77/87 are most likely for windows attestation as we saw a lot of stubbed windows calls in those launches. I want to essentially try all of them."
contributor: "graphify"
outcome: "corrected"
correction: "Do not convert ACH decimal values into AC_LAUNCHMODE. Runtime measurement shows ACH 77, 87, 120, and 128 all inherit the configured default 0x00009609; only 118 lacks a current legitimate selector."
source_nodes: ["ACH Route Selection", "Backend Validation Investigation", "FMracServiceWs Client Request Bridge"]
---

# Q: What other AC launch modes do we have? I know 77/87/118/120 at least. 77/87 are most likely for windows attestation as we saw a lot of stubbed windows calls in those launches. I want to essentially try all of them.

## Answer

Expanded via graph vocabulary: [ach, authentication, compatibility, control, game, launch, proton, route, runtime, selection, steam]. Retained ACH inventory is 77, 87, 118, 120, and 128. Current controlled contracts executed by the official MGL binary reproduced 77, 87, 120, and 128; all four game processes inherited AC_LAUNCHMODE=0x00009609 and stopped before a non-empty Gate0018/MRAC request. ACH and AC_LAUNCHMODE are distinct, and MGL job-mode IDs are not a direct ACH mapping. The Windows-attestation meaning remains unproven. ACH 118 is historical-only and requires a current official route, supported Deck route, or developer cohort for a faithful test.

## Outcome

- Signal: corrected
- Correction: Do not convert ACH decimal values into AC_LAUNCHMODE. Runtime measurement shows ACH 77, 87, 120, and 128 all inherit the configured default 0x00009609; only 118 lacks a current legitimate selector.

## Source Nodes

- ACH Route Selection
- Backend Validation Investigation
- FMracServiceWs Client Request Bridge
