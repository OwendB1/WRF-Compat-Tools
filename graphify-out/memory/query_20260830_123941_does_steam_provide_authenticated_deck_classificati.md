---
type: "bfs"
date: "2026-08-30T12:39:41.145069+00:00"
question: "Does Steam provide authenticated Deck classification, and can the measured CPU topology, RSMB, and storage surfaces be spoofed as the primary acceptance hypothesis?"
contributor: "graphify"
outcome: "corrected"
correction: "Corrected the unsupported authenticated Steam classification claim and ruled out the implemented API-level CPU/RSMB/storage tuple as sufficient."
source_nodes: ["Backend Validation Investigation", "Successful Steam Deck Behavioral Comparison", "GE-Proton10-34-WRF-TLS Runtime Provenance"]
---

# Q: Does Steam provide authenticated Deck classification, and can the measured CPU topology, RSMB, and storage surfaces be spoofed as the primary acceptance hypothesis?

## Answer

Expansion trace: [cpu, deck, proton, security, validation, request, steam]. Steamworks documents only a Deck-or-other-SteamOS boolean, not cryptographic device attestation. Implemented GE-Proton11-6-WRF-DeckProfile-v2 to report Aerith family 23/model 144/stepping 2 through Wine APIs and generated RSMB, with four cores/eight threads. Existing Wine storage behavior already matched one C: extent on disk 0 and a 40-byte SCSI descriptor with no identity offsets. The smoke probe verified that tuple and matching Deck-observed DMA/PCP failures. An official Steam ACH 87 run reached the hangar but closed 1006 after 16.943 seconds, so the API-level CPU/RSMB/storage tuple is not sufficient. Literal CPUID remains host-visible, and exact Deck-side Windows RSMB bytes remain unmeasured.

## Outcome

- Signal: corrected
- Correction: Corrected the unsupported authenticated Steam classification claim and ruled out the implemented API-level CPU/RSMB/storage tuple as sufficient.

## Source Nodes

- Backend Validation Investigation
- Successful Steam Deck Behavioral Comparison
- GE-Proton10-34-WRF-TLS Runtime Provenance