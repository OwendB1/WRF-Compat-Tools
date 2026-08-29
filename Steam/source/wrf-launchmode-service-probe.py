"""Observe the aclaunchapi64 launch-mode response decision without its body."""

from __future__ import annotations

import json
import os
import stat
import time
from pathlib import Path

import gdb


OUTPUT = Path(os.environ["WRF_LAUNCHMODE_PROBE_OUTPUT"]).resolve()
BASE = int(os.environ["WRF_ACLAUNCHAPI_BASE"], 0)
U64_MASK = (1 << 64) - 1
EVENTS: list[dict[str, object]] = []


def reg(name: str) -> int:
    return int(gdb.parse_and_eval(f"${name}")) & U64_MASK


def read_int(address: int, size: int) -> int | None:
    try:
        data = bytes(gdb.selected_inferior().read_memory(address, size))
    except gdb.error:
        return None
    return int.from_bytes(data, "little")


def write_result(outcome: str, **fields: object) -> None:
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    OUTPUT.parent.chmod(stat.S_IRWXU)
    result = {
        "schema": 1,
        "outcome": outcome,
        "time_unix_ns": time.time_ns(),
        "response_body_captured": False,
        "events": EVENTS,
        **fields,
    }
    fd = os.open(OUTPUT, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    try:
        os.write(fd, (json.dumps(result, indent=2, sort_keys=True) + "\n").encode())
    finally:
        os.close(fd)


class DecisionBreakpoint(gdb.Breakpoint):
    def __init__(self, stage: str, rva: int) -> None:
        super().__init__(
            f"*0x{BASE + rva:x}",
            type=gdb.BP_HARDWARE_BREAKPOINT,
            internal=True,
        )
        self.stage = stage
        self.silent = True

    def advance(self, stage: str, rva: int) -> bool:
        self.delete()
        DecisionBreakpoint(stage, rva)
        return False

    def stop(self) -> bool:
        stack = reg("rsp")
        return_code = reg("eax") & 0xFFFFFFFF

        if self.stage == "http_status":
            status = read_int(stack + 0x60, 4)
            EVENTS.append({"stage": self.stage, "return_code": return_code, "status": status})
            if return_code:
                write_result("status-query-error", return_code=return_code)
                return True
            if status != 200:
                write_result("http-status-rejected", http_status=status)
                return True
            return self.advance("content_length", 0x1E728)

        if self.stage == "content_length":
            length = read_int(stack + 0x50, 4)
            EVENTS.append({"stage": self.stage, "return_code": return_code, "bytes": length})
            if return_code:
                write_result("length-query-error", return_code=return_code)
                return True
            if length is None or not 3 <= length <= 126:
                write_result("length-rejected", response_bytes=length)
                return True
            return self.advance("read_complete", 0x1E7A3)

        if self.stage == "read_complete":
            received = read_int(stack + 0x68, 8)
            EVENTS.append({"stage": self.stage, "return_code": return_code, "bytes": received})
            if return_code:
                write_result("read-error", return_code=return_code, received_bytes=received)
                return True
            expected = EVENTS[-2]["bytes"]
            if received != expected:
                write_result(
                    "short-read",
                    expected_bytes=expected,
                    received_bytes=received,
                )
                return True
            return self.advance("hex_parse", 0x1E7D9)

        parsed = return_code
        buffer_address = reg("rbp") + 0x430
        end_address = read_int(stack + 0x58, 8)
        consumed = None if end_address is None else end_address - buffer_address
        terminator = None if end_address is None else read_int(end_address, 1)
        valid = (
            consumed is not None
            and 0 < consumed <= int(EVENTS[1]["bytes"])
            and terminator == 0
        )
        EVENTS.append(
            {
                "stage": self.stage,
                "parsed_value": f"0x{parsed:08x}",
                "consumed_bytes": consumed,
                "nul_terminated": terminator == 0,
            }
        )
        write_result(
            "launch-mode-selected" if valid else "hex-parse-rejected",
            parsed_launch_mode=f"0x{parsed:08x}",
            consumed_bytes=consumed,
            parse_valid=valid,
        )
        return True


DecisionBreakpoint("http_status", 0x1E6F8)
print("WRF launch-mode service probe armed; response bodies are excluded")
