---
type: "publication"
date: "2026-08-08T23:12:47.480087+00:00"
question: "How are the pending MRAC collector and Proton candidate changes published upstream?"
contributor: "graphify"
outcome: "useful"
source_nodes: ["github_workflows_collector_ghcr_yml_collector_ghcr", "source_readme_runtime_provenance", "documentation_03_compatibility_implementation_distributed_steam_runtime"]
---

# Q: How are the pending MRAC collector and Proton candidate changes published upstream?

## Answer

Commit e77ee89 was pushed to production. The Collector GHCR workflow completed successfully and published the tested, signed latest image. The live Portainer deployment still serves agent sha-252ab02ebec6 because compose uses latest without automatic redeployment; its stack must be pulled/redeployed separately. The supported GE runtime split archive intentionally remains unchanged while experimental patches and provenance are published as source.

## Outcome

- Signal: useful

## Source Nodes

- github_workflows_collector_ghcr_yml_collector_ghcr
- source_readme_runtime_provenance
- documentation_03_compatibility_implementation_distributed_steam_runtime