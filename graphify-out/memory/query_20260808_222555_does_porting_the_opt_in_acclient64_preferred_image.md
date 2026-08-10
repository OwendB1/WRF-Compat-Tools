---
type: "implementation"
date: "2026-08-08T22:25:55.399023+00:00"
question: "Does porting the opt-in acclient64 preferred-image-base behavior to the custom GE-Proton10-34 runtime restore Gate0018 MRAC request generation?"
contributor: "graphify"
outcome: "useful"
source_nodes: ["documentation_06_backend_validation_investigation_gate0018_zero_byte_result", "documentation_06_backend_validation_investigation_mrac_service_bridge", "source_readme_runtime_provenance"]
---

# Q: Does porting the opt-in acclient64 preferred-image-base behavior to the custom GE-Proton10-34 runtime restore Gate0018 MRAC request generation?

## Answer

Yes. GE-Proton10-34-WRF-PreferredBase mapped acclient64.dll at 0x180000000 and Normaliz.dll at 0x190000000. Relative to ACClient Online, FMracServiceWs::ClientRequest RPC id 47 was called at +4.896 s, returned at +13.405 s, and the backend closed 1006 at +15.922 s. The port fixes the loader/request-generation boundary but not post-response backend validation.

## Outcome

- Signal: useful

## Source Nodes

- documentation_06_backend_validation_investigation_gate0018_zero_byte_result
- documentation_06_backend_validation_investigation_mrac_service_bridge
- source_readme_runtime_provenance