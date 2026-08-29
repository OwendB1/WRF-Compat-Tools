---
type: "query"
date: "2026-08-10T19:16:30.578347+00:00"
question: "We need stronger deck-specific attestations"
contributor: "graphify"
outcome: "useful"
source_nodes: ["Backend Validation Investigation", "Successful Steam Deck Behavioral Comparison", "GE-Proton10-34-WRF-TLS Runtime Provenance", "Virtual Machine Hardware Identity Limit"]
---

# Q: We need stronger deck-specific attestations

## Answer

Expanded from original query via graph vocab: [deck, hardware, identity, security, machine, device, registry, cpu, comparison, validation, runtime, proton]. Evidence supports a coherent Deck-profile candidate on exact Valve Proton 10.0-4b: retain ACH 118 and the MRAC call/response gate; accumulate host-backed ACPI and TPM-EK APIs, measured Valve/Aerith/Jupiter SMBIOS shape with local test identities, eight logical CPUs, and process-local SteamOS identity while preserving official runtime version metadata and separate provenance. Next observation coverage should target the already observed machine-id read, per-CPU frequency reads, and storage-device probes, plus Secure Boot/UEFI/Code Integrity only if traces show calls. Proton can emulate Windows API surfaces but cannot honestly manufacture a Deck TPM private key, signed quote, genuine serial, or fully intercept native CPUID; do not replay payloads or copy a real Deck identity. A developer-side test allowlist/profile is the correct route for signed attestation.

## Outcome

- Signal: useful

## Source Nodes

- Backend Validation Investigation
- Successful Steam Deck Behavioral Comparison
- GE-Proton10-34-WRF-TLS Runtime Provenance
- Virtual Machine Hardware Identity Limit