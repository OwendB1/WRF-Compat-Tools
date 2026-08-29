---
type: "investigation"
date: "2026-08-29T22:29:00.051735+00:00"
question: "There must be some other hardware/software/firmware attestation that we can gather from the dumps?"
contributor: "graphify"
outcome: "useful"
source_nodes: ["Backend Validation Investigation", "FMracServiceWs Client Request Bridge", "Compatibility Research Scope Boundary", "Payload-Free Evidence Handling"]
---

# Q: There must be some other hardware/software/firmware attestation that we can gather from the dumps?

## Answer

Expanded analysis across paired 2.13 GiB pre/post-encode memory sweeps found global copies of CPU model, readable DMI model fields, two PCI device instance IDs, one UTF-16 storage serial, hostname, kernel release, and machine ID. None appeared in encoder-local changed bytes or the bounded writer graph. Hardware read watches recorded zero reads of the unique host product copy, one machine-ID copy, the original SteamDeck=1 environment value, and its low copied value during the exact encode interval. Raw machine ID appeared once as 16 bytes globally but not locally; MACs and boot ID did not. No saved TPM EK, TPM2/ACPI snapshot chunks, DMI UUID/serial, raw SMBIOS, or meaningful SecureBoot/TPM/firmware markers were found. Eleven requests share only the fixed 9cae0000 prefix; every remaining byte behaves like randomized opaque output. Conclusion: the dumps expose earlier inventory retained in process memory but no plaintext attestation object at final encoding; next evidence should instrument earlier acquisition APIs and device/registry reads.

## Outcome

- Signal: useful

## Source Nodes

- Backend Validation Investigation
- FMracServiceWs Client Request Bridge
- Compatibility Research Scope Boundary
- Payload-Free Evidence Handling