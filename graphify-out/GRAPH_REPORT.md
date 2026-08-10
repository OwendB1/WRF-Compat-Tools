# Graph Report - .  (2026-08-08)

## Corpus Check
- Corpus is ~15,727 words - fits in a single context window. You may not need a graph.

## Summary
- 165 nodes · 361 edges · 12 communities (8 shown, 4 thin omitted)
- Extraction: 94% EXTRACTED · 6% INFERRED · 0% AMBIGUOUS · INFERRED: 21 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- Collector Server APIs
- Collector Configuration
- Compatibility Routes
- Agent Monitoring
- Backend Validation
- Parser Tests
- Enrollment Security
- Collector Deployment
- Proton Installer
- Deck Installer
- Deck Uninstaller
- Go Module

## God Nodes (most connected - your core abstractions)
1. `server` - 27 edges
2. `methodNotAllowed()` - 13 edges
3. `agentState` - 13 edges
4. `event` - 8 edges
5. `serveDownload()` - 7 edges
6. `parseGameLine()` - 7 edges
7. `now()` - 7 edges
8. `WRF Remote Compatibility Collector Guide` - 7 edges
9. `SteamOS QEMU Experiment Plan` - 7 edges
10. `Backend Validation Investigation` - 7 edges

## Surprising Connections (you probably didn't know these)
- `Payload-Free Evidence Handling` --semantically_similar_to--> `Payload-Free Normalized Event Collection`  [INFERRED] [semantically similar]
  documentation/04-evidence-and-security.md → Collector/README.md
- `Steam TLS Relocation Compatibility Fix` --semantically_similar_to--> `Wine Ntdll Stale TLS Pointer Rebase Patch`  [INFERRED] [semantically similar]
  documentation/01-investigation-history.md → Steam/source/README.md
- `Signed Collector Agent Build` --implements--> `Ed25519-Verified Agent Updates`  [INFERRED]
  .github/workflows/collector-ghcr.yml → Collector/README.md
- `WRF Compatibility Tools README` --references--> `WRF Remote Compatibility Collector Guide`  [EXTRACTED]
  README.md → Collector/README.md
- `TestRejectsUnstructuredEvent()` --calls--> `validateEnvelope()`  [INFERRED]
  Collector/main_test.go → Collector/main.go

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Observed ACH Route Family** — documentation_02_route_matrix_ach_77_route, documentation_02_route_matrix_ach_118_route, documentation_02_route_matrix_ach_120_route, documentation_02_route_matrix_ach_128_route [EXTRACTED 1.00]
- **ACH 118 Missing Validation Request Flow** — documentation_06_backend_validation_investigation_gate0018_zero_byte_result, documentation_06_backend_validation_investigation_mrac_service_bridge, documentation_06_backend_validation_investigation_backend_validation_lease, documentation_01_investigation_history_remote_backend_close [EXTRACTED 1.00]
- **Collector Security Model** — collector_readme_payload_free_collection, collector_readme_one_time_enrollment, collector_readme_signed_agent_updates, collector_compose_hardened_container [INFERRED 0.95]

## Communities (12 total, 4 thin omitted)

### Community 0 - "Collector Server APIs"
Cohesion: 0.24
Nodes (8): constantEqual(), methodNotAllowed(), serveDownload(), writeJSON(), server, Mutex, Request, ResponseWriter

### Community 1 - "Collector Configuration"
Cohesion: 0.14
Nodes (23): Client, agentConfig, dashboardData, enrollmentRequest, envelope, defaultConfigPath(), envOr(), healthcheck() (+15 more)

### Community 2 - "Compatibility Routes"
Cohesion: 0.10
Nodes (25): ACH Route Selection, Clean-Room Compatibility Scope, MY.GAMES Identity with Steam Launch Contract, WRF Linux Investigation History, Steam TLS Relocation Compatibility Fix, ACH 118 Steam Deck User-Mode Route, ACH 120 Mixed Authorization Route, ACH 128 Likely Windows-Oriented Route (+17 more)

### Community 3 - "Agent Monitoring"
Cohesion: 0.25
Nodes (13): agentState, event, collectorGap(), inspectGameProcess(), now(), osRelease(), osReleaseAt(), readSmallFile() (+5 more)

### Community 4 - "Backend Validation"
Cohesion: 0.12
Nodes (18): Missing or Late Anti-Cheat Client Request Hypothesis, Repeatable Remote Backend Close, Linux Kernel VFIO Documentation, Safe Phased SteamOS VM Experiment, QEMU x86 CPU Model Documentation, SteamOS QEMU Experiment Plan, SteamOS Userspace A/B Experiment, Valve Steam Deck FAQ (+10 more)

### Community 5 - "Parser Tests"
Cohesion: 0.22
Nodes (16): eventTime(), newRunID(), parseGameLine(), TestCollectorGapIsExplicitAndBounded(), TestEmbeddedUpdatePublicKey(), TestEnrollmentIsOneTimeAndStoresOnlyHash(), TestIngestRequiresTokenAndStoresNormalizedEvent(), TestInviteCSRFToken() (+8 more)

### Community 6 - "Enrollment Security"
Cohesion: 0.27
Nodes (8): clientCredential, credentialRegistry, enrollmentInvite, enrollmentResponse, hashSecret(), pruneInvites(), randomHex(), Time

### Community 7 - "Collector Deployment"
Cohesion: 0.22
Nodes (11): Collector Container Service, Hardened Read-Only Collector Container, Collector Route Authentication Model, One-Time Device Enrollment, Payload-Free Normalized Event Collection, WRF Remote Compatibility Collector Guide, Ed25519-Verified Agent Updates, Payload-Free Evidence Handling (+3 more)

## Ambiguous Edges - Review These
- `ACH Route Selection` → `ACH 128 Likely Windows-Oriented Route`  [AMBIGUOUS]
  documentation/02-route-matrix.md · relation: conceptually_related_to

## Knowledge Gaps
- **22 isolated node(s):** `github.com/OwendB1/WRF-Compat-Tools/Collector`, `enrollmentRequest`, `install.sh script`, `uninstall.sh script`, `GHCR Collector Image Publication` (+17 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **4 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **What is the exact relationship between `ACH Route Selection` and `ACH 128 Likely Windows-Oriented Route`?**
  _Edge tagged AMBIGUOUS (relation: conceptually_related_to) - confidence is low._
- **Why does `server` connect `Collector Server APIs` to `Collector Configuration`, `Enrollment Security`?**
  _High betweenness centrality (0.080) - this node is a cross-community bridge._
- **Why does `WRF Linux Investigation Reference Index` connect `Compatibility Routes` to `Backend Validation`?**
  _High betweenness centrality (0.072) - this node is a cross-community bridge._
- **Why does `WRF Compatibility Tools README` connect `Compatibility Routes` to `Collector Deployment`?**
  _High betweenness centrality (0.041) - this node is a cross-community bridge._
- **What connects `github.com/OwendB1/WRF-Compat-Tools/Collector`, `enrollmentRequest`, `install.sh script` to the rest of the system?**
  _22 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Collector Configuration` be split into smaller, more focused modules?**
  _Cohesion score 0.13666666666666666 - nodes in this community are weakly interconnected._
- **Should `Compatibility Routes` be split into smaller, more focused modules?**
  _Cohesion score 0.10333333333333333 - nodes in this community are weakly interconnected._