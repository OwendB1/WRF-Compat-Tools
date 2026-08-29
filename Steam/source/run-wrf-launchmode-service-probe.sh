#!/usr/bin/env bash
set -euo pipefail
umask 077

expected_shas=(
    "788bbe06565fc99192b5d8cd70767a4cceccb5583c41b98fac08650cda62311b"
    "e98b2b90f967ba0257ba9d492fd8760ca3948c773f6083f015159a21c4aa2d0d"
)
wait_seconds="${WRF_LAUNCHMODE_PROBE_WAIT_SECONDS:-180}"
output_root="${WRF_LAUNCHMODE_PROBE_OUTPUT_ROOT:-/home/owendb/Games/wf-frontiers/launchmode-service-probes}"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"

[[ "$wait_seconds" =~ ^[1-9][0-9]*$ ]] || {
    echo "WRF_LAUNCHMODE_PROBE_WAIT_SECONDS must be a positive integer." >&2
    exit 2
}

gdb_bin="${WRF_LAUNCHMODE_PROBE_GDB:-$(command -v gdb || true)}"
gdb_data_args=()
if [[ -n "${WRF_LAUNCHMODE_PROBE_GDB_DATA:-}" ]]; then
    gdb_data_args=(--data-directory="$WRF_LAUNCHMODE_PROBE_GDB_DATA")
elif [[ -z "$gdb_bin" ]]; then
    private_root="/home/owendb/.local/share/wrf-probe-tools/root2"
    gdb_bin="$private_root/usr/bin/gdb"
    [[ -x "$gdb_bin" ]] || {
        echo "GDB is unavailable. Install it or extract a private copy under $private_root." >&2
        exit 1
    }
    export LD_LIBRARY_PATH="$private_root/usr/lib64:$private_root/lib64${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
    export PYTHONHOME="$private_root/usr"
    gdb_data_args=(--data-directory="$private_root/usr/share/gdb")
fi

find_target() {
    local maps pid map_line
    maps="$(rg -l -i 'aclaunchapi64\.dll' /proc/[0-9]*/maps 2>/dev/null | head -1 || true)"
    [[ -n "$maps" ]] || return 1
    pid="${maps#/proc/}"
    pid="${pid%/maps}"
    map_line="$(awk 'tolower($0) ~ /aclaunchapi64\.dll/ && $3 == "00000000" { print; exit }' "$maps" 2>/dev/null || true)"
    [[ -n "$map_line" ]] || return 1
    printf '%s\t%s\n' "$pid" "$map_line"
}

target=""
deadline=$((SECONDS + wait_seconds))
while [[ -z "$target" ]] && (( SECONDS < deadline )); do
    target="$(find_target || true)"
    [[ -n "$target" ]] || sleep 0.001
done
[[ -n "$target" ]] || {
    echo "Timed out waiting for aclaunchapi64.dll in the launcher job." >&2
    exit 1
}

pid="${target%%$'\t'*}"
map_line="${target#*$'\t'}"
kill -STOP "$pid" 2>/dev/null || {
    echo "The launcher job exited before it could be paused." >&2
    exit 1
}
stopped_pid="$pid"
resume_target() {
    if [[ -n "${stopped_pid:-}" ]]; then
        kill -CONT "$stopped_pid" 2>/dev/null || true
    fi
}
trap resume_target EXIT
trap 'resume_target; exit 130' INT TERM

dll_path="${map_line#* /}"
dll_path="/${dll_path% (deleted)}"
actual_sha="$(sha256sum "$dll_path" | awk '{print $1}')"
expected=false
for candidate in "${expected_shas[@]}"; do
    [[ "$actual_sha" == "$candidate" ]] && expected=true
done
[[ "$expected" == true ]] || {
    echo "Refusing an unknown aclaunchapi64.dll build: $actual_sha" >&2
    printf 'Expected one of:\n' >&2
    printf '  %s\n' "${expected_shas[@]}" >&2
    exit 1
}

module_base="0x${map_line%%-*}"
stamp="$(date +%Y%m%d-%H%M%S)"
output_dir="$output_root/$stamp"
mkdir -p -- "$output_dir"
chmod 700 "$output_dir"
export WRF_LAUNCHMODE_PROBE_OUTPUT="$output_dir/result.json"
export WRF_ACLAUNCHAPI_BASE="$module_base"

printf 'Attaching launch-mode decision probe\nPID: %s\nDLL: %s\nDLL base: %s\nOutput: %s\n' \
    "$pid" "$actual_sha" "$module_base" "$output_dir"

"$gdb_bin" "${gdb_data_args[@]}" -q -nx -batch \
    -ex 'set pagination off' \
    -ex 'set confirm off' \
    -ex 'set debuginfod enabled off' \
    -ex 'handle SIGSTOP SIGSYS SIGSEGV SIGILL SIGBUS SIGFPE SIGUSR1 SIGUSR2 nostop noprint pass' \
    -p "$pid" \
    -x "$script_dir/wrf-launchmode-service-probe.py" \
    -ex continue \
    -ex detach \
    -ex quit
kill -CONT "$pid" 2>/dev/null || true
stopped_pid=""

[[ -s "$output_dir/result.json" ]] || {
    echo "Probe detached without observing the launch-mode decision." >&2
    exit 1
}
chmod -R go-rwx "$output_dir"
sed -n '1,120p' "$output_dir/result.json"
printf 'Launch-mode decision probe complete: %s\n' "$output_dir"
