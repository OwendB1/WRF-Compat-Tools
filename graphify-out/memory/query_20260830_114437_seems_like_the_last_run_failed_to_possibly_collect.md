---
type: "query"
date: "2026-08-30T11:44:37.595163+00:00"
question: "Seems like the last run failed to possibly collect logs but it might still have the information needed https://collect.odb-tech.com/v1/runs/e97a7cfc-ad9c-4b0a-813e-4baf85a321a0"
contributor: "graphify"
outcome: "useful"
source_nodes: ["event", "agentState", ".pollGameLog()", "summarize()", "runSummary", "WRF Remote Compatibility Collector Guide"]
---

# Q: Seems like the last run failed to possibly collect logs but it might still have the information needed https://collect.odb-tech.com/v1/runs/e97a7cfc-ad9c-4b0a-813e-4baf85a321a0

## Answer

Expanded via project vocabulary: collector run event platform_probe diagnostics proton log upload summary. The run contains 1557 events over 10m53.939s with 21 heartbeats, zero collector gaps, normal log lifecycle, backend connected, AC online, ACH 87, and clean shutdown, so collection transport and ordinary game-log watching worked. It lacks all six VeryVerbose startup confirmations and therefore has no MRAC/RPC/ping detail; the diagnostic Engine.ini block was absent or not loaded. Crucially, the authentic successful Steam Deck on Proton 11.0-100 returned DMA Guard 0xc0000003 and PCP_EKPUB 0x80090027, exactly matching the local non-Deck probe. Those statuses are compatible baseline behavior and should not be patched as attestation prerequisites.

## Outcome

- Signal: useful

## Source Nodes

- event
- agentState
- .pollGameLog()
- summarize()
- runSummary
- WRF Remote Compatibility Collector Guide