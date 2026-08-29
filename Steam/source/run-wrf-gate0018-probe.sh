#!/usr/bin/env bash
set -euo pipefail
umask 077

expected_sha="e886b11ed869be07c2aaf7ea0523d7f818ea9e80917e087445c383cc26178b25"
expected_game_sha="1796133e902a2e0d54dc3af0d4532f6febf1c81022d923ba5550999321589e8e"
gate_rva="0x206870"
acclient_size="0x6377000"
wrapper_entry_rva="0x78e0860"
wrapper_decoded_rva="0x78e13f5"
poll_caller_rva="0x78e0de6"
fetch_caller_rva="0x78e128f"
game_pattern='WRFrontiers-Win64-Shipping\.exe'
wait_seconds="${WRF_GATE_PROBE_WAIT_SECONDS:-180}"
output_root="${WRF_GATE_PROBE_OUTPUT_ROOT:-/home/owendb/Games/wf-frontiers/gate0018-probes}"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"

[[ "$wait_seconds" =~ ^[1-9][0-9]*$ ]] || {
    echo "WRF_GATE_PROBE_WAIT_SECONDS must be a positive integer." >&2
    exit 2
}

gdb_bin="${WRF_GATE_PROBE_GDB:-$(command -v gdb || true)}"
gdb_data_args=()
if [[ -n "${WRF_GATE_PROBE_GDB_DATA:-}" ]]; then
    gdb_data_args=(--data-directory="$WRF_GATE_PROBE_GDB_DATA")
elif [[ -z "$gdb_bin" ]]; then
    private_root="/home/owendb/.local/share/wrf-probe-tools/root2"
    gdb_bin="$private_root/usr/bin/gdb"
    [[ -x "$gdb_bin" ]] || {
        echo "GDB is unavailable. Install it or extract a private copy under $private_root." >&2
        exit 1
    }
    export LD_LIBRARY_PATH="$private_root/usr/lib64:$private_root/lib64${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
    gdb_data_args=(--data-directory="$private_root/usr/share/gdb")
fi

pid="${1:-}"
deadline=$((SECONDS + wait_seconds))
while [[ -z "$pid" ]] && (( SECONDS < deadline )); do
    pid="$(pgrep -n -u "$(id -u)" -f -- "$game_pattern" || true)"
    [[ -n "$pid" ]] || sleep 0.25
done
[[ -n "$pid" && -r "/proc/$pid/maps" ]] || {
    echo "Timed out waiting for WRFrontiers-Win64-Shipping.exe." >&2
    exit 1
}

map_line="$(awk 'tolower($0) ~ /acclient64\.dll/ && $3 == "00000000" { print; exit }' "/proc/$pid/maps")"
deadline=$((SECONDS + wait_seconds))
while [[ -z "$map_line" ]] && (( SECONDS < deadline )); do
    sleep 0.25
    [[ -r "/proc/$pid/maps" ]] || break
    map_line="$(awk 'tolower($0) ~ /acclient64\.dll/ && $3 == "00000000" { print; exit }' "/proc/$pid/maps")"
done
[[ -n "$map_line" ]] || {
    echo "acclient64.dll was not mapped before the process exited or timed out." >&2
    exit 1
}

dll_path="${map_line#* /}"
dll_path="/${dll_path}"
dll_path="${dll_path% (deleted)}"
actual_sha="$(sha256sum "$dll_path" | awk '{print $1}')"
[[ "$actual_sha" == "$expected_sha" ]] || {
    echo "Refusing an unknown acclient64.dll build: $actual_sha" >&2
    echo "Expected: $expected_sha" >&2
    exit 1
}

module_base="0x${map_line%%-*}"
game_map_line="$(awk 'tolower($0) ~ /wrfrontiers-win64-shipping\.exe/ && $3 == "00000000" { print; exit }' "/proc/$pid/maps")"
[[ -n "$game_map_line" ]] || {
    echo "Game image base was not found." >&2
    exit 1
}
game_path="${game_map_line#* /}"
game_path="/${game_path}"
actual_game_sha="$(sha256sum "$game_path" | awk '{print $1}')"
[[ "$actual_game_sha" == "$expected_game_sha" ]] || {
    echo "Refusing an unknown game executable build: $actual_game_sha" >&2
    echo "Expected: $expected_game_sha" >&2
    exit 1
}
game_base="0x${game_map_line%%-*}"
stamp="$(date +%Y%m%d-%H%M%S)"
output_dir="$output_root/$stamp"
mkdir -p -- "$output_dir"
chmod 700 "$output_dir"

export WRF_GATE_PROBE_OUTPUT="$output_dir"
export WRF_ACCLIENT_BASE="$module_base"
export WRF_ACCLIENT_SIZE="$acclient_size"
export WRF_GATE0018_RVA="$gate_rva"
export WRF_GAME_BASE="$game_base"
export WRF_GATE0018_WRAPPER_ENTRY_RVA="$wrapper_entry_rva"
export WRF_GATE0018_WRAPPER_DECODED_RVA="$wrapper_decoded_rva"
export WRF_GATE0018_POLL_CALLER_RVA="$poll_caller_rva"
export WRF_GATE0018_FETCH_CALLER_RVA="$fetch_caller_rva"

printf 'Attaching observation-only Gate0018 probe\nPID: %s\nDLL: %s\nDLL base: %s\nGame: %s\nGame base: %s\nOutput: %s\n' \
    "$pid" "$actual_sha" "$module_base" "$actual_game_sha" "$game_base" "$output_dir"

"$gdb_bin" "${gdb_data_args[@]}" -q -nx -batch \
    -ex 'set pagination off' \
    -ex 'set confirm off' \
    -ex 'set debuginfod enabled off' \
    -ex 'set breakpoint pending on' \
    -ex 'handle SIGSYS SIGSEGV SIGILL SIGBUS SIGFPE SIGUSR1 SIGUSR2 nostop noprint pass' \
    -p "$pid" \
    -x "$script_dir/wrf-gate0018-probe.py" \
    -ex continue \
    -ex detach \
    -ex quit

chmod -R go-rwx "$output_dir"
printf 'Gate0018 probe complete: %s\n' "$output_dir"
