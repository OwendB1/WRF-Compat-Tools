"""Observation-only GDB hook for the sanctioned WRF Gate0018 boundary.

The script is loaded after GDB has attached to WRFrontiers-Win64-Shipping.exe.
It records the Windows x64 call/return ABI and bounded vector-like buffers.  It
does not write inferior memory, alter arguments, or replay requests.
"""

from __future__ import annotations

import hashlib
import json
import math
import os
import stat
import time
from pathlib import Path

import gdb


MAX_BUFFER = 64 * 1024
INITIAL_CAPTURE_BYTES = 4096
MAX_POLL_CALLS = 4
GATE_ARGUMENT_BYTES = 0x200
OBJECT_BYTES = 128
POINTER_SIZE = 8
U64_MASK = (1 << 64) - 1


def env_int(name: str) -> int:
    value = os.environ.get(name)
    if not value:
        raise gdb.GdbError(f"{name} is required")
    return int(value, 0)


OUTPUT_DIR = Path(os.environ["WRF_GATE_PROBE_OUTPUT"]).resolve()
MODULE_BASE = env_int("WRF_ACCLIENT_BASE")
MODULE_SIZE = env_int("WRF_ACCLIENT_SIZE")
GATE_RVA = env_int("WRF_GATE0018_RVA")
GATE_ADDRESS = MODULE_BASE + GATE_RVA
GAME_BASE = env_int("WRF_GAME_BASE")
WRAPPER_ENTRY_RVA = env_int("WRF_GATE0018_WRAPPER_ENTRY_RVA")
WRAPPER_DECODED_RVA = env_int("WRF_GATE0018_WRAPPER_DECODED_RVA")
POLL_CALLER_RVA = env_int("WRF_GATE0018_POLL_CALLER_RVA")
FETCH_CALLER_RVA = env_int("WRF_GATE0018_FETCH_CALLER_RVA")
CALLERS = {
    "poll": GAME_BASE + POLL_CALLER_RVA,
    "fetch": GAME_BASE + FETCH_CALLER_RVA,
}
SOURCE_ADDRESS = int(os.environ.get("WRF_GATE_PROBE_SOURCE_ADDRESS", "0"), 0)

OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
OUTPUT_DIR.chmod(stat.S_IRWXU)
EVENTS_PATH = OUTPUT_DIR / "gate0018-events.jsonl"
STACKS_PATH = OUTPUT_DIR / "gate0018-stacks.txt"


def append_private(path: Path, data: bytes) -> None:
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
    try:
        os.write(fd, data)
    finally:
        os.close(fd)


def read_memory(address: int, size: int) -> bytes | None:
    if address < 0x10000 or size < 0 or size > MAX_BUFFER:
        return None
    try:
        return bytes(gdb.selected_inferior().read_memory(address, size))
    except gdb.error:
        return None


def read_u64(address: int) -> int | None:
    data = read_memory(address, POINTER_SIZE)
    return int.from_bytes(data, "little") if data is not None else None


def reg(name: str) -> int:
    return int(gdb.parse_and_eval(f"${name}")) & U64_MASK


def entropy(data: bytes) -> float:
    if not data:
        return 0.0
    counts = [0] * 256
    for byte in data:
        counts[byte] += 1
    length = len(data)
    return -sum((count / length) * math.log2(count / length) for count in counts if count)


def dump_buffer(call_id: int, phase: str, label: str, data: bytes) -> dict[str, object]:
    safe_label = "".join(char if char.isalnum() else "_" for char in label)
    name = f"call-{call_id:02d}-{phase}-{safe_label}.bin"
    path = OUTPUT_DIR / name
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    try:
        os.write(fd, data)
    finally:
        os.close(fd)
    return {
        "artifact": name,
        "bytes": len(data),
        "sha256": hashlib.sha256(data).hexdigest(),
        "entropy_bits_per_byte": round(entropy(data), 4),
    }


def vector_candidates(call_id: int, phase: str, sources: dict[str, int]) -> list[dict[str, object]]:
    found: list[dict[str, object]] = []
    seen: set[tuple[int, int]] = set()
    for source, address in sources.items():
        if address < 0x10000:
            continue
        object_data = read_memory(address, OBJECT_BYTES)
        if object_data is None:
            continue
        for offset in range(0, OBJECT_BYTES - 3 * POINTER_SIZE + 1, POINTER_SIZE):
            begin = int.from_bytes(object_data[offset : offset + 8], "little")
            end = int.from_bytes(object_data[offset + 8 : offset + 16], "little")
            capacity = int.from_bytes(object_data[offset + 16 : offset + 24], "little")
            if not (0x10000 <= begin <= end <= capacity):
                continue
            size = end - begin
            reserved = capacity - begin
            if size > MAX_BUFFER or reserved > 4 * MAX_BUFFER or (begin, size) in seen:
                continue
            seen.add((begin, size))
            item: dict[str, object] = {
                "source": source,
                "object": f"0x{address:x}",
                "offset": offset,
                "begin": f"0x{begin:x}",
                "end": f"0x{end:x}",
                "capacity": f"0x{capacity:x}",
                "bytes": size,
            }
            if size:
                data = read_memory(begin, size)
                if data is not None:
                    item.update(dump_buffer(call_id, phase, f"{source}-off-{offset:x}", data))
            found.append(item)
    return found


def fixed_arguments(call_id: int, phase: str, sources: dict[str, int]) -> list[dict[str, object]]:
    found: list[dict[str, object]] = []
    for register_name in ("rcx", "rdx"):
        source_name = f"entry_{register_name}" if phase == "return" else register_name
        address = sources.get(source_name)
        if address is None:
            continue
        data = read_memory(address, GATE_ARGUMENT_BYTES)
        if data is None:
            continue
        item: dict[str, object] = {
            "source": register_name,
            "address": f"0x{address:x}",
        }
        item.update(dump_buffer(call_id, phase, f"arg-{register_name}", data))
        found.append(item)
    return found


def current_state(
    call_id: int,
    call_type: str,
    phase: str,
    saved_sources: dict[str, int] | None = None,
) -> dict[str, object]:
    registers = {name: reg(name) for name in ("rax", "rbx", "rcx", "rdx", "rsi", "rdi", "r8", "r9", "rsp", "rbp", "rip")}
    sources = {name: registers[name] for name in ("rax", "rcx", "rdx", "r8", "r9", "rsi", "rdi")}
    for index in range(4):
        value = read_u64(registers["rsp"] + 0x28 + index * 8)
        if value is not None:
            sources[f"stack_arg_{index + 5}"] = value
    if saved_sources:
        sources.update({f"entry_{name}": value for name, value in saved_sources.items()})
    return {
        "schema": 1,
        "event": f"gate0018_{phase}",
        "call": call_id,
        "time_unix_ns": time.time_ns(),
        "thread": gdb.selected_thread().global_num,
        "module_base": f"0x{MODULE_BASE:x}",
        "gate_rva": f"0x{GATE_RVA:x}",
        "gate_address": f"0x{GATE_ADDRESS:x}",
        "call_type": call_type,
        "caller_rva": f"0x{POLL_CALLER_RVA if call_type == 'poll' else FETCH_CALLER_RVA:x}",
        "registers": {name: f"0x{value:x}" for name, value in registers.items()},
        "sources": {name: f"0x{value:x}" for name, value in sources.items()},
        "fixed_arguments": fixed_arguments(call_id, phase, sources),
        "vectors": vector_candidates(call_id, phase, sources),
    }


def log_event(event: dict[str, object]) -> None:
    append_private(EVENTS_PATH, (json.dumps(event, sort_keys=True) + "\n").encode())


def log_stack(call_id: int, phase: str) -> None:
    try:
        stack = gdb.execute("bt 20", to_string=True)
    except gdb.error as error:
        stack = f"backtrace unavailable: {error}\n"
    header = f"\n=== call {call_id} {phase} ===\n"
    append_private(STACKS_PATH, (header + stack).encode(errors="replace"))


class GateEntryBreakpoint(gdb.Breakpoint):
    def __init__(self) -> None:
        super().__init__(
            f"*0x{GATE_ADDRESS:x}",
            type=gdb.BP_HARDWARE_BREAKPOINT,
            internal=True,
        )
        self.silent = True
        self.call_id = 0

    def stop(self) -> bool:
        return_address = read_u64(reg("rsp"))
        if return_address is None:
            return False
        call_type = next((name for name, address in CALLERS.items() if address == return_address), None)
        if call_type is None:
            return False
        self.call_id += 1
        state = current_state(self.call_id, call_type, "entry")
        log_event(state)
        if self.call_id == 1:
            log_stack(self.call_id, "entry")
        return False


class BufferWatchpoint(gdb.Breakpoint):
    def __init__(self, address: int, capacity: int, wrapper_call: int, role: str = "destination"):
        super().__init__(
            f"*(unsigned long long *)0x{address:x}",
            type=gdb.BP_WATCHPOINT,
            wp_class=gdb.WP_WRITE,
            internal=True,
        )
        self.silent = True
        self.address = address
        self.capacity = min(capacity, MAX_BUFFER)
        self.wrapper_call = wrapper_call
        self.role = role

    def stop(self) -> bool:
        instruction = reg("rip")
        if not (MODULE_BASE <= instruction < MODULE_BASE + MODULE_SIZE):
            return False
        registers = {
            name: reg(name)
            for name in ("rax", "rbx", "rcx", "rdx", "rsi", "rdi", "r8", "r9", "r10", "r11", "rsp", "rbp", "rip")
        }
        event: dict[str, object] = {
            "schema": 1,
            "event": "acclient_buffer_write",
            "wrapper_call": self.wrapper_call,
            "time_unix_ns": time.time_ns(),
            "thread": gdb.selected_thread().global_num,
            "buffer": f"0x{self.address:x}",
            "capacity": self.capacity,
            "writer": f"0x{instruction:x}",
            "writer_rva": f"0x{instruction - MODULE_BASE:x}",
            "registers": {name: f"0x{value:x}" for name, value in registers.items()},
        }
        data = read_memory(self.address, min(self.capacity, INITIAL_CAPTURE_BYTES))
        if self.role == "source" and (data is None or not any(data[:POINTER_SIZE])):
            return False
        if data is not None:
            event["buffer_state"] = dump_buffer(
                self.wrapper_call, "first-write", f"{self.role}-buffer", data
            )
        event["watch_role"] = self.role
        copy_bytes = registers["r8"]
        if (
            0 < copy_bytes <= self.capacity
            and registers["rcx"] >= POINTER_SIZE
            and registers["rdx"] >= POINTER_SIZE
            and registers["rcx"] - POINTER_SIZE == self.address
        ):
            source_address = registers["rdx"] - POINTER_SIZE
            source = read_memory(source_address, copy_bytes)
            if source is not None:
                copy: dict[str, object] = {
                    "source": f"0x{source_address:x}",
                    "destination": f"0x{self.address:x}",
                }
                copy.update(
                    dump_buffer(
                        self.wrapper_call,
                        "first-write",
                        "copy-source",
                        source,
                    )
                )
                event["copy_candidate"] = copy
        register_buffers = []
        register_bytes = copy_bytes if 0 < copy_bytes <= self.capacity else self.capacity
        for name, address in registers.items():
            if name in ("rsp", "rbp", "rip") or address == self.address:
                continue
            candidate = read_memory(address, register_bytes)
            if candidate is None:
                continue
            item: dict[str, object] = {"source": name, "address": f"0x{address:x}"}
            item.update(
                dump_buffer(
                    self.wrapper_call,
                    "first-write",
                    f"source-{name}",
                    candidate,
                )
            )
            register_buffers.append(item)
        event["register_buffers"] = register_buffers
        log_event(event)
        log_stack(self.wrapper_call, "first-buffer-write")
        self.enabled = False
        return False


class WrapperEntryBreakpoint(gdb.Breakpoint):
    def __init__(self) -> None:
        super().__init__(
            f"*0x{GAME_BASE + WRAPPER_ENTRY_RVA:x}",
            type=gdb.BP_HARDWARE_BREAKPOINT,
            internal=True,
        )
        self.silent = True
        self.calls = 0
        self.active: dict[int, tuple[int, int, int]] = {}
        self.watchpoint: BufferWatchpoint | None = None

    def stop(self) -> bool:
        self.calls += 1
        thread = gdb.selected_thread().global_num
        address = reg("rcx")
        capacity = reg("rdx") & 0xffffffff
        self.active[thread] = (self.calls, address, capacity)
        event: dict[str, object] = {
            "schema": 1,
            "event": "request_wrapper_entry",
            "wrapper_call": self.calls,
            "time_unix_ns": time.time_ns(),
            "thread": thread,
            "buffer": f"0x{address:x}",
            "capacity": capacity,
        }
        if 0 < capacity <= MAX_BUFFER:
            data = read_memory(address, min(capacity, INITIAL_CAPTURE_BYTES))
            if data is not None:
                event["buffer_state"] = dump_buffer(
                    self.calls, "wrapper-entry", "request-buffer", data
                )
            if not SOURCE_ADDRESS:
                if self.watchpoint is not None:
                    self.watchpoint.enabled = False
                self.watchpoint = BufferWatchpoint(address, capacity, self.calls)
        log_event(event)
        return False


class WrapperDecodedBreakpoint(gdb.Breakpoint):
    def __init__(self, owner: WrapperEntryBreakpoint) -> None:
        super().__init__(
            f"*0x{GAME_BASE + WRAPPER_DECODED_RVA:x}",
            type=gdb.BP_HARDWARE_BREAKPOINT,
            internal=True,
        )
        self.silent = True
        self.owner = owner

    def stop(self) -> bool:
        thread = gdb.selected_thread().global_num
        active = self.owner.active.pop(thread, None)
        if active is None:
            return False
        wrapper_call, address, capacity = active
        size = min(reg("rsi"), capacity, MAX_BUFFER)
        event: dict[str, object] = {
            "schema": 1,
            "event": "request_wrapper_decoded",
            "wrapper_call": wrapper_call,
            "time_unix_ns": time.time_ns(),
            "thread": thread,
            "buffer": f"0x{address:x}",
            "capacity": capacity,
            "decoded_bytes": size,
        }
        if size:
            data = read_memory(address, size)
            if data is not None:
                event["request"] = dump_buffer(
                    wrapper_call, "wrapper-decoded", "request", data
                )
        log_event(event)
        if self.owner.watchpoint is not None:
            self.owner.watchpoint.enabled = False
        if size or wrapper_call >= MAX_POLL_CALLS:
            log_stack(wrapper_call, "wrapper-decoded")
            reason = f"{size}-byte request" if size else "poll limit"
            print(f"WRF_GATE_PROBE captured {reason}; detaching")
            return True
        return False


append_private(
    EVENTS_PATH,
    (
        json.dumps(
            {
                "schema": 1,
                "event": "probe_started",
                "time_unix_ns": time.time_ns(),
                "module_base": f"0x{MODULE_BASE:x}",
                "module_size": f"0x{MODULE_SIZE:x}",
                "gate_rva": f"0x{GATE_RVA:x}",
                "gate_address": f"0x{GATE_ADDRESS:x}",
                "game_base": f"0x{GAME_BASE:x}",
                "poll_caller_rva": f"0x{POLL_CALLER_RVA:x}",
                "poll_caller_address": f"0x{CALLERS['poll']:x}",
                "fetch_caller_rva": f"0x{FETCH_CALLER_RVA:x}",
                "fetch_caller_address": f"0x{CALLERS['fetch']:x}",
                "wrapper_entry_rva": f"0x{WRAPPER_ENTRY_RVA:x}",
                "wrapper_decoded_rva": f"0x{WRAPPER_DECODED_RVA:x}",
                "mode": "observation_only",
                "max_buffer_bytes": MAX_BUFFER,
                "max_poll_calls": MAX_POLL_CALLS,
                "gate_argument_bytes": GATE_ARGUMENT_BYTES,
                "source_watch_address": f"0x{SOURCE_ADDRESS:x}" if SOURCE_ADDRESS else None,
            },
            sort_keys=True,
        )
        + "\n"
    ).encode(),
)
GateEntryBreakpoint()
wrapper_entry_breakpoint = WrapperEntryBreakpoint()
if SOURCE_ADDRESS:
    wrapper_entry_breakpoint.watchpoint = BufferWatchpoint(
        SOURCE_ADDRESS, 4096, 0, "source"
    )
WrapperDecodedBreakpoint(wrapper_entry_breakpoint)
print(f"WRF_GATE_PROBE armed hardware breakpoint at 0x{GATE_ADDRESS:x}")
