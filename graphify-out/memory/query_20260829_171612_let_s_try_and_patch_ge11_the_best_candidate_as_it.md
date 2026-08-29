---
type: "query"
date: "2026-08-29T17:16:12.647752+00:00"
question: "Let's try and patch GE11 the best candidate as it seems that attestation is most likely proton agnostic as GE proton is built on valve proton"
contributor: "graphify"
outcome: "corrected"
correction: "Preferred-base loading restored MRAC requests for an older client build, but the current client fails before request generation under both GE-Proton10-34 and the accumulated GE-Proton11-6 candidate."
source_nodes: ["source_readme_ntdll_tls_rebase_patch", "documentation_06_backend_validation_investigation_mrac_service_bridge", "documentation_06_backend_validation_investigation_gate0018_zero_byte_result", "documentation_03_compatibility_implementation_compatibility_implementation", "source_readme_runtime_provenance"]
---

# Q: Let's try and patch GE11 the best candidate as it seems that attestation is most likely proton agnostic as GE proton is built on valve proton

## Answer

Expanded query: proton wine mrac request gate tls runtime client bridge boundary image relocation attestation. Built and ran GE-Proton11-6-WRF-MRAC with preferred-base loading, stale-TLS relocation repair, SystemDmaGuardPolicyInformation class 202, host ACPI reads, host TPM EK PCP_EKPUB, and ACH probes. The protected client mapped at 0x180000000, class 202 succeeded, and it read the real 283-byte TPM EK. It still emitted no FMracServiceWs request: backend close followed AC Online by 8.500 seconds and request sends failed around 70 seconds. The immediately preceding current-build GE-Proton10-34 preferred-base control also emitted no MRAC request, correcting the historical claim that preferred-base alone restores request generation for the current client.

## Outcome

- Signal: corrected
- Correction: Preferred-base loading restored MRAC requests for an older client build, but the current client fails before request generation under both GE-Proton10-34 and the accumulated GE-Proton11-6 candidate.

## Source Nodes

- source_readme_ntdll_tls_rebase_patch
- documentation_06_backend_validation_investigation_mrac_service_bridge
- documentation_06_backend_validation_investigation_gate0018_zero_byte_result
- documentation_03_compatibility_implementation_compatibility_implementation
- source_readme_runtime_provenance