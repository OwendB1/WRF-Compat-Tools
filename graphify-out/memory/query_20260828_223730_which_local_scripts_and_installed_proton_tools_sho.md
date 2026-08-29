---
type: "query"
date: "2026-08-28T22:37:30.688447+00:00"
question: "Which local scripts and installed Proton tools should be used to run the newest native-auth and Steam comparison cycles, and where do their ACH and MRAC capture outputs go?"
contributor: "graphify"
outcome: "useful"
source_nodes: ["Backend Validation Investigation", "WRF Remote Compatibility Collector Guide", "MY.GAMES Identity with Steam Launch Contract", "GE-Proton10-34-WRF-TLS Runtime Provenance"]
---

# Q: Which local scripts and installed Proton tools should be used to run the newest native-auth and Steam comparison cycles, and where do their ACH and MRAC capture outputs go?

## Answer

Expanded from original query via graph vocabulary: [authentication, steam, proton, tools, launch, ach, mrac, capture, collector, runtime, game]. The repository separates launcher routing, Proton runtime selection, and normalized collector evidence. For this test, the native MY.GAMES route used the local native launcher with GE-Proton11-6-WRF-PreferredBase; the Steam route used run-wrf-steam-cycle.sh with the same selected Steam tool. ACH comes from each route MGL main.log; AC, Gate0018, MRAC, and backend-close evidence comes from WRFrontiers.log. The measured Steam cycle wrote redacted results under steam-cycles/20260829-003445. The native direct run selected ACH 77 with no MRAC; the Steam run selected ACH 87 with no MRAC.

## Outcome

- Signal: useful

## Source Nodes

- Backend Validation Investigation
- WRF Remote Compatibility Collector Guide
- MY.GAMES Identity with Steam Launch Contract
- GE-Proton10-34-WRF-TLS Runtime Provenance