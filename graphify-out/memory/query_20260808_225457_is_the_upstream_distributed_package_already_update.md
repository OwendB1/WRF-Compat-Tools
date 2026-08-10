---
type: "status"
date: "2026-08-08T22:54:57.993527+00:00"
question: "Is the upstream distributed package already updated with the GE preferred-base port and collector changes?"
contributor: "graphify"
outcome: "useful"
source_nodes: ["documentation_03_compatibility_implementation_distributed_steam_runtime", "source_readme_runtime_provenance"]
---

# Q: Is the upstream distributed package already updated with the GE preferred-base port and collector changes?

## Answer

No. origin/production remains at 252ab02, while the preferred-base GE patch, collector changes, documentation, and build changes are local and uncommitted. Steam/runtime/SHA256SUMS still names the original GE-Proton10-34-WRF-TLS split archive; GE-Proton10-34-WRF-PreferredBase exists only as a local installed candidate.

## Outcome

- Signal: useful

## Source Nodes

- documentation_03_compatibility_implementation_distributed_steam_runtime
- source_readme_runtime_provenance