---
type: "query"
date: "2026-08-29T23:28:00.737502+00:00"
question: "go ahead and add that collector change as well"
contributor: "graphify"
outcome: "useful"
source_nodes: ["event", "agentState", ".startRun()", ".pollGameProcess()", "WRF Remote Compatibility Collector Guide", "Backend Validation Investigation"]
---

# Q: go ahead and add that collector change as well

## Answer

Expanded from original query via vocab: collector, probe, proton, platform, runtime, event, agent, steam, deck, security. A 2400-token DFS traversal covered 54 nodes. Implemented an embedded signed Windows helper that runs only while the game is idle through app 1491000's configured Proton and prefix, normalizes DMA Guard class 202 and PCP_EKPUB statuses into the existing event pipeline, attaches them to the next run, exposes them in the admin summary, and explicitly excludes EK bytes, hashes, serials, and raw payloads. Cross-builds, tests, vet, shell syntax, and a real local Proton run passed; that run measured 0xc0000003 and 0x80090027.

## Outcome

- Signal: useful

## Source Nodes

- event
- agentState
- .startRun()
- .pollGameProcess()
- WRF Remote Compatibility Collector Guide
- Backend Validation Investigation