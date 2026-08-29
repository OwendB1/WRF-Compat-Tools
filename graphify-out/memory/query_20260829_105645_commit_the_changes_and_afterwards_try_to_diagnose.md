---
type: "investigation"
date: "2026-08-29T10:56:45.228587+00:00"
question: "Commit the changes and afterwards try to diagnose in as much detail as possible what is needed for ACH 118 and a possible different launch mode."
contributor: "graphify"
outcome: "corrected"
correction: "Earlier evidence could not tell whether 0x00009609 was a fallback. The new status/length/read/parse probe proves native ACH 77 and Steam ACH 87 both receive and fully parse a valid service response selecting 0x00009609."
source_nodes: ["ACH Route Selection", "ACH Route and Result Matrix", "ACH 118 Steam Deck User-Mode Route", "MY.GAMES Identity with Steam Launch Contract", "Backend Validation Investigation", "Successful Steam Deck Behavioral Comparison"]
---

# Q: Commit the changes and afterwards try to diagnose in as much detail as possible what is needed for ACH 118 and a possible different launch mode.

## Answer

Expanded via graph vocabulary [ach, launch, mode, selection, route, contract, authentication, steam, deck, service, config, backend]. Live native ACH 77 and Steam ACH 87 controls both received HTTP 200 from the launch-mode service, declared/read 10 bytes, fully parsed 0x00009609, and passed that value to the game. Thus 0x00009609 is actively service-selected in both routes, not merely a failed-request fallback, and AC_LAUNCHMODE is not the ACH selector. Current Steam 87 retains channel 47, SteamAppId 1491000, and -nosplash -LaunchFromSteam like historical official Steam 118; native and Steam MGL.exe are byte-identical. A faithful 118 run therefore requires an upstream official launcher/backend selection, signed route, policy/cohort, or supported Deck route. The historical MY.GAMES hybrid explicitly rewrote MGL-selected 120 to 118 and cannot reveal the genuine selector. Autonomous native Play and intro skipping plus the payload-free decision probe are now integrated into the boundary runner.

## Outcome

- Signal: corrected
- Correction: Earlier evidence could not tell whether 0x00009609 was a fallback. The new status/length/read/parse probe proves native ACH 77 and Steam ACH 87 both receive and fully parse a valid service response selecting 0x00009609.

## Source Nodes

- ACH Route Selection
- ACH Route and Result Matrix
- ACH 118 Steam Deck User-Mode Route
- MY.GAMES Identity with Steam Launch Contract
- Backend Validation Investigation
- Successful Steam Deck Behavioral Comparison