---
type: "query"
date: "2026-08-30T12:18:59.265064+00:00"
question: "Can we attest/copy/mock all of the deck information from the latest capture to see if the game is then able to run and then start narrowing down which possible DMI/TPM etc might be responsible for giving off the positive signal?"
contributor: "graphify"
outcome: "useful"
source_nodes: ["Successful Steam Deck Behavioral Comparison"]
---

# Q: Can we attest/copy/mock all of the deck information from the latest capture to see if the game is then able to run and then start narrowing down which possible DMI/TPM etc might be responsible for giving off the positive signal?

## Answer

Expanded from original query via graph vocab: [deck, hardware, identity, security, machine, device, registry, cpu, comparison, validation, runtime, proton]. Implemented GE-Proton11-6-WRF-DeckProfile with every non-secret captured Deck surface that can be represented safely: Valve/Aerith/Jupiter DMI strings with local synthetic identities, TPM2 ACPI presence and size with DMAR and IVRS absent, eight logical CPUs, captured DXGI IDs, SteamOS 3.8.16 build 20260716.1, and authentic Deck failure states for DMA Guard class 202 and PCP_EKPUB. No genuine Deck serial, UUID, EK, private key, signed quote, raw SMBIOS or ACPI bytes, direct CPUID, or authenticated Steam device state was copied. Full, none, DMI, ACPI, and CPU plus GPU plus OS controls all closed with 1006 in the same 16.648 to 17.309 second post-connect band. One initial full run generated RPC 47 and a response but an immediate full repeat did not, so it is not reproducible evidence for a DMI or TPM prerequisite. The captured and mockable metadata is not a sufficient positive signal. Next work should target raw SMBIOS and storage topology observation, CPU identity versus direct CPUID, and authenticated Steam or signed device state; a developer test cohort or allowlist is required if the discriminator is cryptographic.

## Outcome

- Signal: useful

## Source Nodes

- Successful Steam Deck Behavioral Comparison