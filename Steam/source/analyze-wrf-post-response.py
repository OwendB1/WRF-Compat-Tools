#!/usr/bin/env python3
"""Align narrow Wine acclient probe events with a game-log boundary."""

import argparse
import re
import shlex
from collections import Counter, defaultdict
from datetime import datetime, timezone
from pathlib import Path

FILETIME_EPOCH = 116_444_736_000_000_000
GAME_STAMP = re.compile(r"^\[(\d{4}\.\d{2}\.\d{2}-\d{2}\.\d{2}\.\d{2}:\d{3})\]")


def parse_game_time(line):
    match = GAME_STAMP.match(line)
    if not match:
        return None
    return datetime.strptime(match.group(1), "%Y.%m.%d-%H.%M.%S:%f").replace(tzinfo=timezone.utc)


def first_reference(path, requested):
    markers = {
        "mrac-response": ("RPC Response", "FMracServiceWs::ClientRequest"),
        "backend-close": ("Connection was closed with:",),
        "ac-online": ("ACClient: Online",),
    }
    wanted = markers if requested == "auto" else {requested: markers[requested]}
    found = {}
    with path.open(errors="replace") as source:
        for line in source:
            for name, tokens in wanted.items():
                if name not in found and all(token in line for token in tokens):
                    stamp = parse_game_time(line)
                    if stamp:
                        found[name] = stamp
    for name in wanted:
        if name in found:
            return name, found[name]
    raise ValueError(f"no {requested} reference found in {path}")


def read_probe(path):
    events = []
    with path.open(errors="replace") as source:
        for line_number, line in enumerate(source, 1):
            marker = line.find("WRFPROBE ")
            if marker < 0:
                continue
            fields = {}
            for token in shlex.split(line[marker + len("WRFPROBE "):]):
                if "=" in token:
                    key, value = token.split("=", 1)
                    fields[key] = value
            try:
                ticks = int(fields["filetime"])
            except (KeyError, ValueError):
                continue
            fields["at"] = datetime.fromtimestamp(
                (ticks - FILETIME_EPOCH) / 10_000_000, timezone.utc
            )
            fields["line"] = str(line_number)
            events.append(fields)
    return events


def describe(event):
    preferred = ("type", "state", "tid", "reason", "rva", "code", "access", "target",
                 "status", "retval")
    return " ".join(f"{key}={event[key]}" for key in preferred if key in event)


def scan_slices(events):
    faults = [event for event in events
              if event.get("type") == "exception" and "target" in event]
    slices = []
    for event in faults:
        if not slices or (event["at"] - slices[-1][-1]["at"]).total_seconds() > 0.2:
            slices.append([])
        slices[-1].append(event)
    return slices


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--proton-log", required=True, type=Path)
    parser.add_argument("--game-log", required=True, type=Path)
    parser.add_argument("--before", type=float, default=5.0)
    parser.add_argument("--after", type=float, default=5.0)
    parser.add_argument("--reference", choices=("auto", "mrac-response", "backend-close", "ac-online"),
                        default="auto")
    parser.add_argument("--game-time-offset", type=float, default=0.0,
                        help="seconds added to game timestamps if a host is not logging UTC")
    args = parser.parse_args()

    reference_name, reference_time = first_reference(args.game_log, args.reference)
    reference = reference_time.timestamp() + args.game_time_offset
    events = read_probe(args.proton_log)
    selected = []
    for event in events:
        delta = event["at"].timestamp() - reference
        if -args.before <= delta <= args.after:
            selected.append((delta, event))

    print(f"Reference ({reference_name}): "
          f"{datetime.fromtimestamp(reference, timezone.utc).isoformat()}")
    print(f"Probe events: {len(events)} total, {len(selected)} in window")
    for delta, event in selected:
        if event.get("type") != "exception":
            print(f"{delta:+8.3f}s line={event['line']} {describe(event)}")

    exceptions = Counter((event.get("code"), event.get("rva"), event.get("access"))
                         for _, event in selected if event.get("type") == "exception")
    if exceptions:
        print(f"Exceptions in window: {sum(exceptions.values())}")
        for (code, rva, access), count in exceptions.most_common():
            print(f"  count={count} code={code} rva={rva} access={access}")

    slices = scan_slices(events)
    if slices:
        unique_targets = {int(event["target"], 0) for fault_slice in slices
                          for event in fault_slice}
        print(f"Fault-target scan: {len(unique_targets)} unique targets in "
              f"{len(slices)} timed slices")
        for fault_slice in slices:
            targets = [int(event["target"], 0) for event in fault_slice]
            start = fault_slice[0]["at"].timestamp() - reference
            end = fault_slice[-1]["at"].timestamp() - reference
            print(f"  delta={start:+.3f}..{end:+.3f}s count={len(fault_slice)} "
                  f"target={min(targets):#x}..{max(targets):#x}")

    attached = defaultdict(list)
    lifetimes = []
    for event in events:
        if event.get("type") != "thread":
            continue
        tid = event.get("tid", "unknown")
        if event.get("state") == "attach":
            attached[tid].append(event["at"])
        elif event.get("state") == "detach" and attached[tid]:
            start = attached[tid].pop(0)
            if (start.timestamp() <= reference + args.after and
                    event["at"].timestamp() >= reference - args.before):
                lifetimes.append((tid, start, event["at"]))

    if lifetimes:
        print("Thread lifetimes near response:")
        for tid, start, end in lifetimes:
            print(f"  tid={tid} duration={(end - start).total_seconds():.3f}s "
                  f"attach_delta={start.timestamp() - reference:+.3f}s "
                  f"detach_delta={end.timestamp() - reference:+.3f}s")

    if not events:
        raise SystemExit("no WRFPROBE events found; enable +wrfprobe in WINEDEBUG")
    if not selected:
        raise SystemExit("no probe events overlap the reference window; check the game-log timezone")


if __name__ == "__main__":
    main()
