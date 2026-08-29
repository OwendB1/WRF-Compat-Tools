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
HANDOFF_BYTES = 4096
PRE_ENVELOPE_WRITER_RVAS = {0x4E405C4, 0x26050C, 0x260555}
MAX_PRIVATE_SWEEP_BYTES = 8 * 1024 * 1024 * 1024
MAX_ACQUISITION_CALLS = 512


def identity_probe_patterns() -> dict[str, bytes]:
    patterns = {"steamdeck_env": b"SteamDeck=1"}
    for label, path in (
        ("host_product_name", Path("/sys/class/dmi/id/product_name")),
        ("machine_id", Path("/etc/machine-id")),
    ):
        try:
            value = path.read_bytes().strip()
        except OSError:
            continue
        if len(value) >= POINTER_SIZE:
            patterns[label] = value
    return patterns


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
HANDOFF_RVA = int(os.environ.get("WRF_GATE_PROBE_HANDOFF_RVA", "0"), 0)
PROBE_MODE = os.environ.get("WRF_GATE_PROBE_MODE", "wrapper")
IDENTITY_WATCH_PROFILE = os.environ.get("WRF_IDENTITY_WATCH_PROFILE", "host")

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


def read_u32(address: int) -> int | None:
    data = read_memory(address, 4)
    return int.from_bytes(data, "little") if data is not None else None


def reg(name: str) -> int:
    return int(gdb.parse_and_eval(f"${name}")) & U64_MASK


def call_argument(index: int, stack_pointer: int | None = None) -> int | None:
    """Read one Windows x64 ABI argument at function entry."""
    registers = ("rcx", "rdx", "r8", "r9")
    if 1 <= index <= len(registers):
        return reg(registers[index - 1])
    if index < 5:
        return None
    if stack_pointer is None:
        stack_pointer = reg("rsp")
    return read_u64(stack_pointer + 0x28 + (index - 5) * POINTER_SIZE)


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


def mapping_for(address: int) -> dict[str, object] | None:
    """Return non-sensitive /proc mapping metadata for one address."""
    try:
        lines = Path(f"/proc/{gdb.selected_inferior().pid}/maps").read_text().splitlines()
    except OSError:
        return None
    for line in lines:
        fields = line.split(maxsplit=5)
        start_text, end_text = fields[0].split("-", 1)
        start, end = int(start_text, 16), int(end_text, 16)
        if start <= address < end:
            return {
                "start": f"0x{start:x}",
                "end": f"0x{end:x}",
                "bytes": end - start,
                "permissions": fields[1],
                "file_offset": fields[2],
                "path": fields[5] if len(fields) == 6 else None,
            }
    return None


def image_base_for_suffix(suffix: str) -> int | None:
    """Return the zero-offset image base for a mapped file suffix."""
    try:
        lines = Path(f"/proc/{gdb.selected_inferior().pid}/maps").read_text().splitlines()
    except OSError:
        return None
    suffix = suffix.lower()
    for line in lines:
        fields = line.split(maxsplit=5)
        if (
            len(fields) == 6
            and fields[2] == "00000000"
            and fields[5].lower().endswith(suffix)
        ):
            return int(fields[0].split("-", 1)[0], 16)
    return None


def read_utf16z(address: int, limit: int = 1024) -> bytes | None:
    data = read_memory(address, limit)
    if data is None:
        return None
    for offset in range(0, len(data) - 1, 2):
        if data[offset : offset + 2] == b"\0\0":
            return data[: offset + 2]
    return data


def read_unicode_string(address: int) -> bytes | None:
    header = read_memory(address, 16)
    if header is None:
        return None
    length = int.from_bytes(header[:2], "little")
    buffer = int.from_bytes(header[8:16], "little")
    if not length or length > 4096:
        return b"" if not length else None
    return read_memory(buffer, length)


def read_object_name(address: int) -> bytes | None:
    data = read_memory(address, 48)
    if data is None:
        return None
    unicode_string = int.from_bytes(data[16:24], "little")
    return read_unicode_string(unicode_string) if unicode_string else b""


ACQUISITION_SPECS: tuple[dict[str, object], ...] = (
    {"name": "NtQuerySystemInformation", "status": "nt", "codes": (1,),
     "outputs": (("system-information", 2, ("arg", 3), ("ptr32", 4), 1),)},
    {"name": "NtQuerySystemInformationEx", "status": "nt", "codes": (1,),
     "inputs": (("query-input", 2, 3),),
     "outputs": (("system-information-ex", 4, ("arg", 5), ("ptr32", 6), 1),)},
    {"name": "NtQuerySystemEnvironmentValueEx", "status": "nt",
     "texts": ((1, "unicode", "environment-name"),),
     "outputs": (("environment-value", 3, ("ptr32-entry", 4), ("ptr32", 4), 1),)},
    {"name": "NtOpenKey", "status": "nt", "texts": ((3, "object", "key-path"),),
     "outputs": (("key-handle", 1, ("fixed", 8), ("capacity", 0), 1),)},
    {"name": "NtOpenKeyEx", "status": "nt", "texts": ((3, "object", "key-path"),),
     "outputs": (("key-handle", 1, ("fixed", 8), ("capacity", 0), 1),)},
    {"name": "NtQueryKey", "status": "nt", "codes": (2,),
     "outputs": (("key-information", 3, ("arg", 4), ("ptr32", 5), 1),)},
    {"name": "NtQueryValueKey", "status": "nt", "codes": (3,),
     "texts": ((2, "unicode", "value-name"),),
     "outputs": (("value-information", 4, ("arg", 5), ("ptr32", 6), 1),)},
    {"name": "NtEnumerateKey", "status": "nt", "codes": (2, 3),
     "outputs": (("enumerated-key", 4, ("arg", 5), ("ptr32", 6), 1),)},
    {"name": "NtEnumerateValueKey", "status": "nt", "codes": (2, 3),
     "outputs": (("enumerated-value", 4, ("arg", 5), ("ptr32", 6), 1),)},
    {"name": "NtCreateFile", "status": "nt", "texts": ((3, "object", "file-path"),),
     "outputs": (("file-handle", 1, ("fixed", 8), ("capacity", 0), 1),)},
    {"name": "NtOpenFile", "status": "nt", "texts": ((3, "object", "file-path"),),
     "outputs": (("file-handle", 1, ("fixed", 8), ("capacity", 0), 1),)},
    {"name": "NtDeviceIoControlFile", "status": "nt", "codes": (6,),
     "inputs": (("device-input", 7, 8),),
     "outputs": (("device-output", 9, ("arg", 10), ("iosb", 5), 1),)},
    {"name": "DeviceIoControl", "status": "bool", "codes": (2,),
     "inputs": (("device-input", 3, 4),),
     "outputs": (("device-output", 5, ("arg", 6), ("ptr32", 7), 1),)},
    {"name": "GetSystemFirmwareTable", "status": "length", "codes": (1, 2),
     "outputs": (("firmware-table", 3, ("arg", 4), ("rax32", 0), 1),)},
    {"name": "GetFirmwareEnvironmentVariableExW", "status": "length", "codes": (4,),
     "texts": ((1, "utf16", "firmware-variable"), (2, "utf16", "vendor-guid")),
     "outputs": (("firmware-variable", 3, ("arg", 4), ("rax32", 0), 1),)},
    {"name": "GetNativeSystemInfo", "status": "void",
     "outputs": (("native-system-info", 1, ("fixed", 48), ("capacity", 0), 1),)},
    {"name": "GetSystemInfo", "status": "void",
     "outputs": (("system-info", 1, ("fixed", 48), ("capacity", 0), 1),)},
    {"name": "GetLogicalProcessorInformationEx", "status": "bool", "codes": (1,),
     "outputs": (("logical-processors", 2, ("ptr32-entry", 3), ("ptr32", 3), 1),)},
    {"name": "GetPhysicallyInstalledSystemMemory", "status": "bool",
     "outputs": (("physical-memory", 1, ("fixed", 8), ("capacity", 0), 1),)},
    {"name": "GetComputerNameExW", "status": "bool", "codes": (1,),
     "outputs": (("computer-name", 2, ("ptr32-entry", 3), ("ptr32", 3), 2),)},
    {"name": "GetVolumeInformationW", "status": "bool",
     "outputs": (("volume-name", 2, ("arg", 3), ("capacity", 0), 2),
                 ("volume-serial", 4, ("fixed", 4), ("capacity", 0), 1),
                 ("max-component", 5, ("fixed", 4), ("capacity", 0), 1),
                 ("filesystem-flags", 6, ("fixed", 4), ("capacity", 0), 1),
                 ("filesystem-name", 7, ("arg", 8), ("capacity", 0), 2))},
    {"name": "GetAdaptersAddresses", "status": "zero", "codes": (1, 2),
     "outputs": (("adapter-addresses", 5, ("ptr32-entry", 4), ("ptr32", 4), 1),)},
    {"name": "Tbsi_Context_Create", "status": "zero",
     "inputs": (("tbs-context-params", 1, ("fixed", 20)),),
     "outputs": (("tbs-context", 2, ("fixed", 8), ("capacity", 0), 1),)},
    {"name": "Tbsip_Submit_Command", "status": "zero", "codes": (2, 3),
     "inputs": (("tpm-command", 4, 5),),
     "outputs": (("tpm-response", 6, ("ptr32-entry", 7), ("ptr32", 7), 1),)},
    {"name": "BCryptGetProperty", "status": "zero", "codes": (6,),
     "texts": ((2, "utf16", "bcrypt-property"),),
     "outputs": (("bcrypt-property", 3, ("arg", 4), ("ptr32", 5), 1),)},
    {"name": "NCryptGetProperty", "status": "zero", "codes": (6,),
     "texts": ((2, "utf16", "ncrypt-property"),),
     "outputs": (("ncrypt-property", 3, ("arg", 4), ("ptr32", 5), 1),)},
    {"name": "NCryptOpenStorageProvider", "status": "zero", "codes": (3,),
     "texts": ((2, "utf16", "storage-provider"),),
     "outputs": (("provider-handle", 1, ("fixed", 8), ("capacity", 0), 1),)},
    {"name": "NCryptOpenKey", "status": "zero", "codes": (4, 5),
     "texts": ((3, "utf16", "key-name"),),
     "outputs": (("key-handle", 2, ("fixed", 8), ("capacity", 0), 1),)},
    {"name": "SetupDiGetDeviceRegistryPropertyW", "status": "bool", "codes": (3,),
     "outputs": (("device-registry-property", 5, ("arg", 6), ("ptr32", 7), 1),)},
    {"name": "SetupDiGetDeviceInstanceIdW", "status": "bool",
     "outputs": (("device-instance-id", 3, ("arg", 4), ("ptr32", 5), 2),)},
    {"name": "CM_Get_Device_IDW", "status": "zero", "codes": (1, 4),
     "outputs": (("configuration-device-id", 2, ("arg", 3), ("capacity", 0), 2),)},
    {"name": "CM_Get_DevNode_Registry_PropertyW", "status": "zero", "codes": (1, 2, 6),
     "outputs": (("configuration-property", 4, ("ptr32-entry", 5), ("ptr32", 5), 1),)},
)


class AcquisitionReturnBreakpoint(gdb.Breakpoint):
    def __init__(
        self,
        owner: "AcquisitionBreakpoint",
        entry: dict[str, object],
        return_address: int,
        thread: int,
    ) -> None:
        if owner.owner.active_returns >= 3:
            raise gdb.GdbError("hardware return-breakpoint budget is exhausted")
        super().__init__(
            f"*0x{return_address:x}",
            type=gdb.BP_HARDWARE_BREAKPOINT,
            internal=True,
        )
        self.silent = True
        self.owner = owner
        self.entry = entry
        self.condition = f"$_gthread == {thread}"
        owner.owner.active_returns += 1

    def stop(self) -> bool:
        self.owner.capture_return(self.entry)
        self.owner.owner.active_returns -= 1
        self.enabled = False
        return False

class AcquisitionBreakpoint(gdb.Breakpoint):
    """Capture API inputs and outputs only when the caller stack reaches acclient."""

    def __init__(self, owner: "AcquisitionTrace", spec: dict[str, object]) -> None:
        super().__init__(str(spec["name"]), internal=True)
        self.silent = True
        self.owner = owner
        self.spec = spec

    def stop(self) -> bool:
        module_base, acclient_rva = self.owner.acclient_caller()
        if module_base is None or acclient_rva is None:
            return False
        self.owner.ensure_handoff(module_base)
        if self.owner.calls >= MAX_ACQUISITION_CALLS:
            return False

        self.owner.calls += 1
        call_id = self.owner.calls
        stack_pointer = reg("rsp")
        args = [call_argument(index, stack_pointer) or 0 for index in range(1, 11)]
        entry: dict[str, object] = {
            "call": call_id,
            "spec": self.spec,
            "args": args,
            "stack_pointer": stack_pointer,
            "entry_u32": {},
        }
        for output in self.spec.get("outputs", ()):
            capacity = output[2]
            if capacity[0] == "ptr32-entry":
                pointer = args[int(capacity[1]) - 1]
                entry["entry_u32"][int(capacity[1])] = read_u32(pointer) or 0

        event: dict[str, object] = {
            "schema": 3,
            "event": "acclient_acquisition_entry",
            "call": call_id,
            "time_unix_ns": time.time_ns(),
            "thread": gdb.selected_thread().global_num,
            "api": self.spec["name"],
            "acclient_caller_rva": f"0x{acclient_rva:x}",
            "arguments": [f"0x{value:x}" for value in args],
            "codes": {
                str(index): f"0x{args[index - 1] & 0xffffffff:x}"
                for index in self.spec.get("codes", ())
            },
        }
        text_artifacts = []
        for index, kind, label in self.spec.get("texts", ()):
            pointer = args[index - 1]
            if kind == "utf16":
                data = read_utf16z(pointer)
            elif kind == "unicode":
                data = read_unicode_string(pointer)
            else:
                data = read_object_name(pointer)
            if data is not None:
                text_artifacts.append(
                    dump_buffer(call_id, "acquisition-entry", f"{self.spec['name']}-{label}", data)
                )
        event["text_artifacts"] = text_artifacts

        input_artifacts = []
        for label, pointer_index, length_source in self.spec.get("inputs", ()):
            pointer = args[pointer_index - 1]
            if isinstance(length_source, tuple):
                length = int(length_source[1])
            else:
                length = args[int(length_source) - 1]
            data = read_memory(pointer, min(length, MAX_BUFFER)) if length else b""
            if data is not None:
                input_artifacts.append(
                    dump_buffer(call_id, "acquisition-entry", f"{self.spec['name']}-{label}", data)
                )
        event["input_artifacts"] = input_artifacts
        log_event(event)
        log_stack(call_id, f"acquisition-entry-{self.spec['name']}")
        return_address = read_u64(stack_pointer)
        try:
            if return_address is None:
                raise gdb.GdbError("return address is unreadable")
            AcquisitionReturnBreakpoint(
                self,
                entry,
                return_address,
                gdb.selected_thread().global_num,
            )
        except gdb.error as error:
            log_event({
                "schema": 3,
                "event": "acclient_acquisition_return_unavailable",
                "call": call_id,
                "time_unix_ns": time.time_ns(),
                "api": self.spec["name"],
                "error": str(error),
            })
        return False

    @staticmethod
    def _capacity(entry: dict[str, object], source: tuple[str, int], scale: int) -> int:
        kind, value = source
        args = entry["args"]
        if kind == "fixed":
            capacity = value
        elif kind == "arg":
            capacity = args[value - 1]
        else:
            capacity = entry["entry_u32"].get(value, 0)
        return min(int(capacity) * scale, MAX_BUFFER)

    @staticmethod
    def _returned(entry: dict[str, object], source: tuple[str, int], capacity: int, scale: int) -> int:
        kind, value = source
        args = entry["args"]
        if kind == "ptr32":
            pointer = args[value - 1]
            if pointer < 0x10000:
                return capacity
            result = read_u32(pointer) or 0
        elif kind == "rax32":
            result = reg("rax") & 0xffffffff
        elif kind == "iosb":
            result = read_u64(args[value - 1] + 8) or 0
        else:
            return capacity
        return min(int(result) * scale, capacity)

    def capture_return(self, entry: dict[str, object]) -> None:
        status = reg("rax") & 0xffffffff
        status_kind = self.spec.get("status")
        success = (
            status_kind == "void"
            or (status_kind == "nt" and not status & 0x80000000)
            or (status_kind == "zero" and status == 0)
            or (status_kind in ("bool", "length") and status != 0)
        )
        outputs = []
        for label, pointer_index, capacity_source, returned_source, scale in self.spec.get("outputs", ()):
            pointer = entry["args"][pointer_index - 1]
            capacity = self._capacity(entry, capacity_source, scale)
            returned = self._returned(entry, returned_source, capacity, scale)
            item: dict[str, object] = {
                "label": label,
                "pointer": f"0x{pointer:x}",
                "capacity_bytes": capacity,
                "returned_bytes": returned,
            }
            if success and pointer >= 0x10000 and returned:
                data = read_memory(pointer, returned)
                if data is not None:
                    item.update(
                        dump_buffer(
                            int(entry["call"]), "acquisition-return",
                            f"{self.spec['name']}-{label}", data,
                        )
                    )
            outputs.append(item)
        log_event({
            "schema": 3,
            "event": "acclient_acquisition_return",
            "call": entry["call"],
            "time_unix_ns": time.time_ns(),
            "thread": gdb.selected_thread().global_num,
            "api": self.spec["name"],
            "status": f"0x{status:08x}",
            "success": success,
            "outputs": outputs,
        })

class AcquisitionTrace:
    def __init__(self) -> None:
        self.calls = 0
        self.active_returns = 0
        self.handoff: HandoffBreakpoint | None = None
        self.module_base = MODULE_BASE or None
        self.next_module_check_ns = 0
        # ponytail: avoid a GDB 16 pending-breakpoint crash; the Nt* hooks retain
        # firmware-variable and TPM device traffic until those DLLs are loaded.
        unstable_pending = {
            "GetFirmwareEnvironmentVariableExW",
            "Tbsi_Context_Create",
            "Tbsip_Submit_Command",
        }
        specs = (
            spec for spec in ACQUISITION_SPECS
            if PROBE_MODE != "early-acquisition" or spec["name"] not in unstable_pending
        )
        self.breakpoints = [AcquisitionBreakpoint(self, spec) for spec in specs]

    def ensure_handoff(self, module_base: int) -> None:
        if self.handoff is None and HANDOFF_RVA:
            self.handoff = HandoffBreakpoint(module_base)

    def acclient_caller(self) -> tuple[int | None, int | None]:
        module_base = self.module_base
        if module_base is None:
            now = time.monotonic_ns()
            if now < self.next_module_check_ns:
                return None, None
            self.next_module_check_ns = now + 10_000_000
            module_base = image_base_for_suffix("acclient64.dll")
            self.module_base = module_base
        if module_base is None:
            return None, None
        frame = gdb.newest_frame()
        for _ in range(48):
            if frame is None:
                break
            try:
                pc = int(frame.pc()) & U64_MASK
            except gdb.error:
                pc = 0
            if module_base <= pc < module_base + MODULE_SIZE:
                return module_base, pc - module_base
            try:
                frame = frame.older()
            except gdb.error:
                break
        return module_base, None


def dump_private_writable_memory(label: str) -> dict[str, object]:
    """Dump every private read/write mapping while the pre-envelope thread is stopped."""
    pid = gdb.selected_inferior().pid
    mappings: list[tuple[int, int, str, str | None]] = []
    for line in Path(f"/proc/{pid}/maps").read_text().splitlines():
        fields = line.split(maxsplit=5)
        if fields[1] != "rw-p":
            continue
        start_text, end_text = fields[0].split("-", 1)
        mappings.append(
            (
                int(start_text, 16),
                int(end_text, 16),
                fields[1],
                fields[5] if len(fields) == 6 else None,
            )
        )
    virtual_bytes = sum(end - start for start, end, _, _ in mappings)
    if virtual_bytes > MAX_PRIVATE_SWEEP_BYTES:
        raise OSError(f"private writable sweep is too large: {virtual_bytes} bytes")

    data_path = OUTPUT_DIR / f"pre-envelope-{label}-memory.bin"
    manifest_path = OUTPUT_DIR / f"pre-envelope-{label}-memory.json"
    records: list[dict[str, object]] = []
    patterns = identity_probe_patterns()
    pattern_hits: dict[str, list[int]] = {name: [] for name in patterns}
    longest_pattern = max(map(len, patterns.values()), default=1)
    mem_fd = os.open(f"/proc/{pid}/mem", os.O_RDONLY)
    out_fd = os.open(data_path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    file_offset = 0
    try:
        for start, end, permissions, path in mappings:
            dumped = 0
            error = None
            address = start
            overlap = b""
            while address < end:
                size = min(1024 * 1024, end - address)
                try:
                    chunk = os.pread(mem_fd, size, address)
                except OSError as read_error:
                    error = str(read_error)
                    break
                if not chunk:
                    error = "short read"
                    break
                searchable = overlap + chunk
                searchable_address = address - len(overlap)
                for name, pattern in patterns.items():
                    cursor = 0
                    while True:
                        match = searchable.find(pattern, cursor)
                        if match < 0:
                            break
                        hit = searchable_address + match
                        if hit >= start and hit not in pattern_hits[name]:
                            pattern_hits[name].append(hit)
                        cursor = match + 1
                overlap = searchable[-(longest_pattern - 1) :]
                view = memoryview(chunk)
                while view:
                    written = os.write(out_fd, view)
                    view = view[written:]
                dumped += len(chunk)
                address += len(chunk)
            records.append(
                {
                    "start": f"0x{start:x}",
                    "end": f"0x{end:x}",
                    "permissions": permissions,
                    "path": path,
                    "file_offset": file_offset,
                    "bytes": dumped,
                    "error": error,
                }
            )
            file_offset += dumped
    finally:
        os.close(out_fd)
        os.close(mem_fd)
    manifest_path.write_text(json.dumps({"schema": 1, "mappings": records}))
    manifest_path.chmod(stat.S_IRUSR | stat.S_IWUSR)
    return {
        "artifact": data_path.name,
        "manifest": manifest_path.name,
        "bytes": file_offset,
        "mapping_count": len(records),
        "identity_value_locations": {
            name: [f"0x{address:x}" for address in addresses]
            for name, addresses in pattern_hits.items()
        },
    }


def exact_pointer_offsets(data: bytes, values: dict[str, int]) -> dict[str, list[int]]:
    matches: dict[str, list[int]] = {}
    for label, value in values.items():
        needle = value.to_bytes(POINTER_SIZE, "little")
        offsets: list[int] = []
        cursor = 0
        while len(offsets) < 64:
            offset = data.find(needle, cursor)
            if offset < 0:
                break
            offsets.append(offset)
            cursor = offset + 1
        if offsets:
            matches[label] = offsets
    return matches


class HandoffBreakpoint(gdb.Breakpoint):
    """Capture the final acclient request copy before the game sees it."""

    def __init__(self, module_base: int = MODULE_BASE) -> None:
        self.module_base = module_base
        super().__init__(
            f"*0x{module_base + HANDOFF_RVA:x}",
            type=gdb.BP_HARDWARE_BREAKPOINT,
            internal=True,
        )
        self.silent = True
        self.hits = 0

    def stop(self) -> bool:
        self.hits += 1
        copy_bytes = reg("r8")
        source = reg("rdx")
        destination = reg("rcx")
        return_address = read_u64(reg("rsp"))
        if (
            copy_bytes != HANDOFF_BYTES
            or return_address is None
            or not (
                self.module_base
                <= return_address
                < self.module_base + MODULE_SIZE
            )
        ):
            return False

        source_data = read_memory(source, copy_bytes)
        if source_data is None:
            return False
        call_id = 1
        event: dict[str, object] = {
            "schema": 2,
            "event": "acclient_request_handoff",
            "call": call_id,
            "time_unix_ns": time.time_ns(),
            "thread": gdb.selected_thread().global_num,
            "handoff_rva": f"0x{HANDOFF_RVA:x}",
            "return_address": f"0x{return_address:x}",
            "return_rva": f"0x{return_address - self.module_base:x}",
            "source": f"0x{source:x}",
            "destination": f"0x{destination:x}",
            "bytes": copy_bytes,
            "source_mapping": mapping_for(source),
            "destination_mapping": mapping_for(destination),
            "request": dump_buffer(call_id, "handoff-entry", "request-source", source_data),
            "registers": {
                name: f"0x{reg(name):x}"
                for name in (
                    "rax", "rbx", "rcx", "rdx", "rsi", "rdi", "r8", "r9",
                    "r10", "r11", "r12", "r13", "r14", "r15", "rsp", "rbp", "rip",
                )
            },
        }

        stack_base = max(0x10000, reg("rsp") - 0x8000)
        stack_data = read_memory(stack_base, 0x10000)
        if stack_data is not None:
            event["stack_window"] = dump_buffer(
                call_id, "handoff-entry", "stack-window", stack_data
            )
            event["stack_pointer_references"] = {
                "base": f"0x{stack_base:x}",
                "matches": exact_pointer_offsets(
                    stack_data,
                    {
                        "source": source,
                        "source_plus_8": source + 8,
                        "destination": destination,
                        "return_address": return_address,
                    },
                ),
            }

        code_base = max(self.module_base, return_address - 0x200)
        code_data = read_memory(code_base, 0x600)
        if code_data is not None:
            event["runtime_caller_code"] = {
                "base": f"0x{code_base:x}",
                **dump_buffer(call_id, "handoff-entry", "runtime-caller-code", code_data),
            }
        try:
            instructions = gdb.execute(
                f"x/160i 0x{return_address - 0x180:x}", to_string=True
            )
            append_private(
                STACKS_PATH,
                (
                    f"\n=== request handoff caller RVA 0x{return_address - self.module_base:x} ===\n"
                    + instructions
                ).encode(errors="replace"),
            )
        except gdb.error as error:
            event["runtime_disassembly_error"] = str(error)

        log_event(event)
        log_stack(call_id, "request-handoff")
        print(
            "WRF_GATE_PROBE captured 4096-byte acclient handoff "
            f"from caller RVA 0x{return_address - self.module_base:x}; detaching"
        )
        return True


class IdentityReadWatchpoint(gdb.Breakpoint):
    """Report whether the protected encoder reads one concrete identity value."""

    def __init__(self, label: str, address: int, module_base: int) -> None:
        watch_address = (address + 3) & ~3
        super().__init__(
            f"*(unsigned int *)0x{watch_address:x}",
            type=gdb.BP_WATCHPOINT,
            wp_class=gdb.WP_READ,
            internal=True,
        )
        self.silent = True
        self.label = label
        self.address = address
        self.watch_address = watch_address
        self.module_base = module_base
        self.condition = (
            f"$pc >= 0x{module_base:x} && $pc < 0x{module_base + MODULE_SIZE:x}"
        )

    def stop(self) -> bool:
        reader = reg("rip")
        log_event(
            {
                "schema": 2,
                "event": "identity_value_read",
                "time_unix_ns": time.time_ns(),
                "thread": gdb.selected_thread().global_num,
                "label": self.label,
                "address": f"0x{self.address:x}",
                "watch_address": f"0x{self.watch_address:x}",
                "reader": f"0x{reader:x}",
                "reader_rva": f"0x{reader - self.module_base:x}",
            }
        )
        log_stack(0, f"identity-value-read-{self.label}")
        self.enabled = False
        return False


class ProducerWatchpoint(gdb.Breakpoint):
    """Record early writes to a candidate request allocation."""

    def __init__(self, owner: "AllocationBreakpoint", address: int, base: int, size: int):
        super().__init__(
            f"*(unsigned long long *)0x{address:x}",
            type=gdb.BP_WATCHPOINT,
            wp_class=gdb.WP_WRITE,
            internal=True,
        )
        self.silent = True
        self.owner = owner
        self.allocation = owner.allocations
        self.address = address
        self.base = base
        self.size = size
        self.hits = 0

    def stop(self) -> bool:
        self.hits += 1
        max_hits = 4 if PROBE_MODE == "early-mmap" else 16
        if self.hits > max_hits:
            self.enabled = False
            return False
        writer = reg("rip")
        capture_size = min(self.size, 0x4000)
        data = read_memory(self.base, capture_size)
        writer_mapping = mapping_for(writer)
        module_base = image_base_for_suffix("acclient64.dll")
        writer_in_acclient = bool(
            module_base is not None
            and module_base <= writer < module_base + MODULE_SIZE
        )
        writer_rva = writer - module_base if writer_in_acclient and module_base else None
        event: dict[str, object] = {
            "schema": 2,
            "event": "candidate_request_write",
            "allocation": self.allocation,
            "write": self.hits,
            "time_unix_ns": time.time_ns(),
            "thread": gdb.selected_thread().global_num,
            "allocation_base": f"0x{self.base:x}",
            "allocation_bytes": self.size,
            "watch_address": f"0x{self.address:x}",
            "writer": f"0x{writer:x}",
            "writer_mapping": writer_mapping,
            "writer_in_acclient": writer_in_acclient,
            "writer_rva": f"0x{writer_rva:x}" if writer_rva is not None else None,
            "registers": {
                name: f"0x{reg(name):x}"
                for name in (
                    "rax", "rbx", "rcx", "rdx", "rsi", "rdi", "r8", "r9",
                    "r10", "r11", "r12", "r13", "r14", "r15", "rsp", "rbp", "rip",
                )
            },
        }
        if data is not None:
            event["buffer_state"] = dump_buffer(
                self.allocation,
                f"producer-write-{self.hits}",
                f"candidate-{self.address - self.base:x}",
                data,
            )
        sweep_labels = {0x4E405C4: "write2", 0x26050C: "write3"}
        if writer_rva in sweep_labels:
            sweep_label = sweep_labels[writer_rva]
            sweep_path = OUTPUT_DIR / f"pre-envelope-{sweep_label}-memory.bin"
            if not sweep_path.exists():
                try:
                    event["process_memory_sweep"] = dump_private_writable_memory(sweep_label)
                except OSError as error:
                    event["process_memory_sweep_error"] = str(error)
        if writer_rva == 0x4E405C4 and module_base is not None:
            handoff = getattr(self.owner, "handoff", None)
            if handoff is not None:
                handoff.enabled = False
            for watchpoint in getattr(self.owner, "watchpoints", []):
                if watchpoint is not self:
                    watchpoint.enabled = False
            locations = event.get("process_memory_sweep", {}).get(
                "identity_value_locations", {}
            )
            if IDENTITY_WATCH_PROFILE == "deck":
                requested_watches = (
                    ("steamdeck_env_low", "steamdeck_env", 0),
                    ("steamdeck_env_original", "steamdeck_env", -1),
                )
            else:
                requested_watches = (
                    ("host_product_name", "host_product_name", 0),
                    ("machine_id", "machine_id", 0),
                )
            choices = []
            for watch_label, value_label, index in requested_watches:
                addresses = locations.get(value_label, [])
                if not addresses:
                    continue
                choices.append((watch_label, int(addresses[index], 16)))
            identity_watchpoints = []
            for label, address in choices:
                try:
                    identity_watchpoints.append(
                        IdentityReadWatchpoint(label, address, module_base)
                    )
                except gdb.error:
                    break
            setattr(self.owner, "identity_watchpoints", identity_watchpoints)
            event["identity_read_watchpoints"] = [
                {"label": watchpoint.label, "address": f"0x{watchpoint.address:x}"}
                for watchpoint in identity_watchpoints
            ]
            if not identity_watchpoints and handoff is not None:
                handoff.enabled = True
        if writer_rva == 0x26050C:
            for watchpoint in getattr(self.owner, "identity_watchpoints", []):
                watchpoint.enabled = False
            handoff = getattr(self.owner, "handoff", None)
            if handoff is not None:
                handoff.enabled = True
        if writer_rva in PRE_ENVELOPE_WRITER_RVAS:
            snapshots: list[dict[str, object]] = []
            pointees: list[dict[str, object]] = []
            seen: set[int] = set()
            seen_pointees: set[int] = set()
            for name in (
                "rax", "rbx", "rcx", "rdx", "rsi", "rdi", "r8", "r9",
                "r10", "r12", "r13", "r14", "r15", "rbp", "rsp",
            ):
                address = reg(name)
                mapping = mapping_for(address)
                if (
                    address in seen
                    or not mapping
                    or not str(mapping.get("permissions", "")).startswith("rw")
                ):
                    continue
                seen.add(address)
                available = int(str(mapping["end"]), 16) - address
                pointer_data = read_memory(address, min(4096, available))
                if pointer_data is not None:
                    snapshots.append(
                        {
                            "register": name,
                            "address": f"0x{address:x}",
                            "mapping": mapping,
                            **dump_buffer(
                                self.allocation,
                                f"producer-write-{self.hits}",
                                f"{name}-pointer",
                                pointer_data,
                            ),
                        }
                    )
                    if writer_rva == 0x4E405C4 and name == "rsp":
                        for offset in range(0, len(pointer_data) - 7, 8):
                            value = int.from_bytes(pointer_data[offset : offset + 8], "little")
                            value_mapping = mapping_for(value)
                            if (
                                len(pointees) >= 48
                                or value in seen_pointees
                                or not value_mapping
                                or not str(value_mapping.get("permissions", "")).startswith("rw")
                            ):
                                continue
                            seen_pointees.add(value)
                            available = int(str(value_mapping["end"]), 16) - value
                            pointee_data = read_memory(value, min(512, available))
                            if pointee_data is not None:
                                pointees.append(
                                    {
                                        "source_register": name,
                                        "source_offset": offset,
                                        "address": f"0x{value:x}",
                                        "mapping": value_mapping,
                                        **dump_buffer(
                                            self.allocation,
                                            f"producer-write-{self.hits}",
                                            f"{name}-{offset:x}-pointee",
                                            pointee_data,
                                        ),
                                    }
                                )
            event["pointer_snapshots"] = snapshots
            event["pointee_snapshots"] = pointees
        log_event(event)
        try:
            instructions = gdb.execute(f"x/96i 0x{writer - 0xc0:x}", to_string=True)
            append_private(
                STACKS_PATH,
                (
                    f"\n=== candidate allocation {self.owner.allocations} write "
                    f"{self.hits} writer 0x{writer:x} ===\n" + instructions
                ).encode(errors="replace"),
            )
        except gdb.error:
            pass
        log_stack(self.allocation, f"candidate-write-{self.hits}")
        if self.hits >= max_hits:
            self.enabled = False
        return False


class AllocationReturnBreakpoint(gdb.Breakpoint):
    def __init__(
        self,
        owner: "AllocationBreakpoint",
        return_address: int,
        base_pointer: int,
        size_pointer: int,
        requested: int,
    ) -> None:
        super().__init__(
            f"*0x{return_address:x}",
            type=gdb.BP_HARDWARE_BREAKPOINT,
            internal=True,
            temporary=True,
        )
        self.silent = True
        self.owner = owner
        self.base_pointer = base_pointer
        self.size_pointer = size_pointer
        self.requested = requested

    def stop(self) -> bool:
        base = read_u64(self.base_pointer)
        size = read_u64(self.size_pointer)
        status = reg("rax") & 0xffffffff
        if status or base is None or size is None or size < 0x2000:
            self.owner.enabled = True
            return False
        self.owner.arm_candidate(base, size, self.requested)
        return False


class AllocationBreakpoint(gdb.Breakpoint):
    """Follow transient request-sized NtAllocateVirtualMemory regions."""

    CANDIDATE_SIZES = {0x2000}

    def __init__(self) -> None:
        super().__init__(
            "NtAllocateVirtualMemory",
            type=gdb.BP_HARDWARE_BREAKPOINT,
            internal=True,
        )
        self.silent = True
        self.allocations = 0
        self.watchpoints: list[ProducerWatchpoint] = []

    def arm_candidate(self, base: int, size: int, requested: int) -> None:
        if self.allocations:
            return
        self.allocations += 1
        log_event(
            {
                "schema": 2,
                "event": "candidate_request_allocation",
                "allocation": self.allocations,
                "time_unix_ns": time.time_ns(),
                "thread": gdb.selected_thread().global_num,
                "base": f"0x{base:x}",
                "bytes": size,
                "requested_bytes": requested,
                "mapping": mapping_for(base),
            }
        )
        self.watchpoints = [ProducerWatchpoint(self, base, base, size)]

    def stop(self) -> bool:
        size_pointer = reg("r9")
        requested = read_u64(size_pointer)
        if requested not in self.CANDIDATE_SIZES:
            return False
        try:
            maps = Path(f"/proc/{gdb.selected_inferior().pid}/maps").read_text()
        except OSError:
            return False
        if "acclient64.dll" not in maps.lower():
            return False
        try:
            stack = gdb.execute("bt 12", to_string=True)
        except gdb.error:
            return False
        return_address = read_u64(reg("rsp"))
        base_pointer = reg("rdx")
        if return_address is None or base_pointer < 0x10000:
            return False
        append_private(
            STACKS_PATH,
            ("\n=== acclient 8192-byte allocation ===\n" + stack).encode(
                errors="replace"
            ),
        )
        self.enabled = False
        AllocationReturnBreakpoint(
            self, return_address, base_pointer, size_pointer, requested
        )
        return False


class MmapTrace:
    """Follow rotating 8 KiB Unix mappings from process startup."""

    MAX_ACTIVE_WATCHPOINTS = 3

    def __init__(self) -> None:
        self.allocations = 0
        self.watchpoints: list[ProducerWatchpoint] = []
        self.pending: set[int] = set()
        self.handoff: HandoffBreakpoint | None = None
        gdb.execute("catch syscall mmap", to_string=True)
        breakpoints = gdb.breakpoints() or ()
        self.catchpoint = breakpoints[-1]
        gdb.execute(
            f"condition {self.catchpoint.number} $rsi == 8192",
            to_string=True,
        )
        gdb.execute(
            f"commands {self.catchpoint.number}\n"
            "silent\n"
            "python mmap_trace_hit()\n"
            "continue\n"
            "end",
            to_string=True,
        )

    def hit(self) -> None:
        if self.handoff is None:
            module_base = image_base_for_suffix("acclient64.dll")
            if module_base is not None:
                self.handoff = HandoffBreakpoint(module_base)
        thread = gdb.selected_thread().global_num
        result = reg("rax")
        if result == ((-38) & U64_MASK):
            self.pending.add(thread)
            return
        if thread not in self.pending:
            return
        self.pending.remove(thread)
        if result >= ((-4095) & U64_MASK) or result < 0x10000:
            return
        self.allocations += 1
        mapping = mapping_for(result)
        log_event(
            {
                "schema": 2,
                "event": "candidate_request_mmap",
                "allocation": self.allocations,
                "time_unix_ns": time.time_ns(),
                "thread": thread,
                "base": f"0x{result:x}",
                "bytes": 0x2000,
                "mapping": mapping,
            }
        )
        if not (
            result < 0x100000000
            and mapping
            and mapping.get("path") is None
            and mapping.get("bytes") == 0x2000
            and str(mapping.get("permissions", "")).startswith("rw")
        ):
            return
        self.watchpoints = [
            watchpoint
            for watchpoint in self.watchpoints
            if watchpoint.is_valid() and watchpoint.enabled
        ]
        for watchpoint in self.watchpoints:
            if watchpoint.base == result:
                watchpoint.enabled = False
        self.watchpoints = [
            watchpoint for watchpoint in self.watchpoints if watchpoint.enabled
        ]
        if len(self.watchpoints) >= self.MAX_ACTIVE_WATCHPOINTS:
            return
        try:
            self.watchpoints.append(ProducerWatchpoint(self, result, result, 0x2000))
        except gdb.error as error:
            log_event(
                {
                    "schema": 2,
                    "event": "candidate_watchpoint_error",
                    "allocation": self.allocations,
                    "time_unix_ns": time.time_ns(),
                    "base": f"0x{result:x}",
                    "error": str(error),
                }
            )


MMAP_TRACE: MmapTrace | None = None


def mmap_trace_hit() -> None:
    if MMAP_TRACE is not None:
        MMAP_TRACE.hit()


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
                "probe_mode": PROBE_MODE,
                "identity_watch_profile": IDENTITY_WATCH_PROFILE,
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
if PROBE_MODE in ("acquisition", "early-acquisition"):
    if not HANDOFF_RVA:
        raise gdb.GdbError("WRF_GATE_PROBE_HANDOFF_RVA is required in acquisition mode")
    ACQUISITION_TRACE = AcquisitionTrace()
    if MODULE_BASE:
        ACQUISITION_TRACE.ensure_handoff(MODULE_BASE)
    print(
        "WRF_GATE_PROBE armed acclient-attributed acquisition APIs and request handoff"
    )
elif PROBE_MODE in ("handoff", "producer"):
    if not HANDOFF_RVA:
        raise gdb.GdbError("WRF_GATE_PROBE_HANDOFF_RVA is required in handoff mode")
    HandoffBreakpoint()
    if PROBE_MODE == "producer":
        AllocationBreakpoint()
    print(
        "WRF_GATE_PROBE armed request handoff breakpoint at "
        f"0x{MODULE_BASE + HANDOFF_RVA:x}"
    )
elif PROBE_MODE == "early-producer":
    AllocationBreakpoint()
    print("WRF_GATE_PROBE armed early transient-allocation producer trace")
elif PROBE_MODE == "early-mmap":
    MMAP_TRACE = MmapTrace()
    print("WRF_GATE_PROBE armed early 8192-byte mmap producer trace")
else:
    GateEntryBreakpoint()
    wrapper_entry_breakpoint = WrapperEntryBreakpoint()
    if SOURCE_ADDRESS:
        wrapper_entry_breakpoint.watchpoint = BufferWatchpoint(
            SOURCE_ADDRESS, 4096, 0, "source"
        )
    WrapperDecodedBreakpoint(wrapper_entry_breakpoint)
    print(f"WRF_GATE_PROBE armed hardware breakpoint at 0x{GATE_ADDRESS:x}")
