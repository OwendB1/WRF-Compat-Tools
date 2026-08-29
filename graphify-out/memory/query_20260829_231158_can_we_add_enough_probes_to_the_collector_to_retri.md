---
type: "query"
date: "2026-08-29T23:11:58.246089+00:00"
question: "Can we add enough probes to the collector to retrieve the information needed to simulate a steam deck?"
contributor: "graphify"
outcome: "useful"
source_nodes: ["runAgent()", ".startRun()", "WRF Remote Compatibility Collector Guide", "Payload-Free Normalized Event Collection", "Compatibility Research Scope Boundary", "Payload-Free Evidence Handling", "Backend Validation Investigation"]
---

# Q: Can we add enough probes to the collector to retrieve the information needed to simulate a steam deck?

## Answer

Expanded from original query via vocab: [runAgent, pollProbe, startRun, runSummary, WRF Remote Compatibility Collector Guide, Payload-Free Normalized Event Collection, Compatibility Research Scope Boundary, Payload-Free Evidence Handling, Backend Validation Investigation]. Extended the once-per-run platform_profile with privacy-safe CPU signature/topology, SMBIOS structure and table-size metadata, bounded non-unique storage descriptors/topology, EFI Secure Boot lockdown and IOMMU state, TPM version/driver/readability, GPU driver/IOMMU attachment, and SteamOS build/variant metadata. Unique serials, UUID values, machine ID, raw SMBIOS/firmware, storage WWIDs, EFI contents, and TPM keys remain excluded. Tests, vet, build, and diff checks pass.

## Outcome

- Signal: useful

## Source Nodes

- runAgent()
- .startRun()
- WRF Remote Compatibility Collector Guide
- Payload-Free Normalized Event Collection
- Compatibility Research Scope Boundary
- Payload-Free Evidence Handling
- Backend Validation Investigation