---
type: "investigation"
date: "2026-08-08T22:46:18.095572+00:00"
question: "What should be checked next after the GE preferred-base port restores Gate0018 MRAC call and response but the backend closes about 2.5 seconds later?"
contributor: "graphify"
outcome: "useful"
source_nodes: ["documentation_06_backend_validation_investigation_successful_steam_deck_comparison", "documentation_06_backend_validation_investigation_mrac_service_bridge", "documentation_03_compatibility_implementation_named_pipe_job_observation", "documentation_05_steamos_vm_plan_steamos_userspace_ab"]
---

# Q: What should be checked next after the GE preferred-base port restores Gate0018 MRAC call and response but the backend closes about 2.5 seconds later?

## Answer

Prioritize a same-build Deck-versus-desktop comparison: collect SHA-256 for acclient64.dll and aclaunchapi64.dll with the new 128 MiB ceiling, then capture payload-free post-response exception, transient-thread/TLS callback, and GC_PIPE_NAME open timing on both. If binaries match, run a SteamOS-userspace control before any further Wine patch. Packet payload inspection and blind loader patches are low value because MRAC transport now succeeds. Prepare a sanitized vendor report from the call/response/close evidence.

## Outcome

- Signal: useful

## Source Nodes

- documentation_06_backend_validation_investigation_successful_steam_deck_comparison
- documentation_06_backend_validation_investigation_mrac_service_bridge
- documentation_03_compatibility_implementation_named_pipe_job_observation
- documentation_05_steamos_vm_plan_steamos_userspace_ab