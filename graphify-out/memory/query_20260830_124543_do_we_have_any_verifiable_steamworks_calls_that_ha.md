---
type: "query"
date: "2026-08-30T12:45:43.981678+00:00"
question: "Do we have any verifiable steamworks calls that happen during the time the game is active? I'd like to very much know if we can observe any calls to steamworks IsSteamRunningOnSteamDeck"
contributor: "graphify"
outcome: "useful"
source_nodes: ["Valve ISteamUtils API Documentation", "Backend Validation Investigation", "Successful Steam Deck Behavioral Comparison"]
---

# Q: Do we have any verifiable steamworks calls that happen during the time the game is active? I'd like to very much know if we can observe any calls to steamworks IsSteamRunningOnSteamDeck

## Answer

Expanded from original query via graph vocabulary: [steam, deck, api, client, game, runtime, probe, request, utils, validation]. A temporary GE11 lsteamclient trace observed exactly one SteamUtils010 IsSteamRunningOnSteamDeck invocation. Its Wine thread 0300 was tied to 32-bit MGLauncher MGL.exe by adjacent module paths and 32-bit interface addresses. The 64-bit game process was Wine PID 0420 with Steamworks threads 0424 and 0488. During startup, hangar, and disconnect it made thousands of verifiable Steamworks calls including RunFrame, GetAppID, GetSteamID, BLoggedOn, GetAuthSessionTicket, RequestCurrentStats, and lobby/friends calls, but zero IsSteamRunningOnSteamDeck calls. Therefore the API is observable, but in this run it was launcher-only and occurred before the game process started.

## Outcome

- Signal: useful

## Source Nodes

- Valve ISteamUtils API Documentation
- Backend Validation Investigation
- Successful Steam Deck Behavioral Comparison