---
type: "debugging"
date: "2026-08-08T19:05:47.787269+00:00"
question: "Why does the exact Valve Proton 10 WRF build still hang during login after rebasing stale TLS callbacks?"
contributor: "graphify"
outcome: "useful"
---

# Q: Why does the exact Valve Proton 10 WRF build still hang during login after rebasing stale TLS callbacks?

## Answer

The callback patch was active, but acclient64.dll next executed stale preferred-base address 0x180611d49 while mapped high. Wine builtin Normaliz.dll already occupied the shared preferred base 0x180000000. Added an opt-in exact-name loader experiment: WRF_PREFER_ACCLIENT_BASE=1 moves 64-bit Normaliz requested base to 0x190000000 and lets acclient64 try 0x180000000, with normal relocation fallback. Both ntdll architectures build cleanly; isolated loader smoke tests confirmed both branches.

## Outcome

- Signal: useful