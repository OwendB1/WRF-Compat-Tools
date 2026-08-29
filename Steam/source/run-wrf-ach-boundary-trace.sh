#!/usr/bin/env bash
set -euo pipefail
umask 077
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

usage() {
    cat <<'EOF'
Usage: run-wrf-ach-boundary-trace.sh steam|native [timeout-seconds]

Launch WRF and stop an observation-only trace after the launcher has selected
ACH and the game has inherited AC_LAUNCHMODE. The trace records connection
metadata only: it does not record HTTP/TLS bodies, command lines, or tokens.

The launched game is left running. Raw strace output remains private and local.
EOF
}

[[ $# -ge 1 && $# -le 2 ]] || { usage >&2; exit 2; }
route="$1"
timeout_seconds="${2:-120}"
[[ "$timeout_seconds" =~ ^[1-9][0-9]*$ ]] || { usage >&2; exit 2; }

steam_root="${WRF_STEAM_ROOT:-$HOME/.local/share/Steam}"
native_root="${WRF_NATIVE_ROOT:-/home/owendb/Games/wf-frontiers}"
output_root="${WRF_ACH_TRACE_OUTPUT_ROOT:-$native_root/ach-boundary-traces}"
strace_bin="${WRF_ACH_TRACE_STRACE:-$(command -v strace || true)}"

case "$route" in
    steam)
        game_root="$steam_root/steamapps/common/WRFrontiers/13_2017027/WRFrontiers"
        launcher="$steam_root/steamapps/common/WRFrontiers/MGLauncher/MGL.exe"
        launcher_log="$steam_root/steamapps/common/WRFrontiers/MGLauncher/main.log"
        acl="$game_root/Binaries/Win64/aclaunchapi64.dll"
        cfg="$game_root/Binaries/Win64/acclient.cfg"
        proton_log="$HOME/steam-1491000.log"
        launch=(steam -applaunch 1491000)
        ;;
    native)
        game_root="$native_root/drive_c/MY.GAMES/War Robots Frontiers/WRFrontiers"
        launcher="$native_root/drive_c/MY.GAMES/MY.GAMES Launcher/MGL.exe"
        launcher_log="$native_root/drive_c/MY.GAMES/MY.GAMES Launcher/main.log"
        acl="$game_root/Binaries/Win64/aclaunchapi64.dll"
        cfg="$game_root/Binaries/Win64/acclient.cfg"
        proton_log="$native_root/steam-1491000.log"
        native_tool="${WRF_PROTON_TOOL:-$steam_root/compatibilitytools.d/GE-Proton11-6-WRF-ACHProbe}"
        launch=(env PROTONPATH="$native_tool" PROTON_LOG=1 PROTON_LOG_DIR="$native_root"
            WINEDEBUG="${WRF_ACH_WINEDEBUG:-warn+module,+wrfach}"
            "$native_root/launch-wrf-mygames-deck.sh")
        ;;
    -h|--help)
        usage
        exit 0
        ;;
    *)
        usage >&2
        exit 2
        ;;
esac

for command_name in objdump rg sha256sum; do
    command -v "$command_name" >/dev/null || {
        echo "Required command not found: $command_name" >&2
        exit 1
    }
done
[[ -n "$strace_bin" && -x "$strace_bin" ]] || {
    echo "strace not found; set WRF_ACH_TRACE_STRACE to its path" >&2
    exit 1
}
[[ -f "$launcher" ]] || { echo "Launcher not found: $launcher" >&2; exit 1; }
[[ -f "$launcher_log" ]] || { echo "Launcher log not found: $launcher_log" >&2; exit 1; }
[[ -f "$acl" ]] || { echo "Anti-cheat launcher not found: $acl" >&2; exit 1; }
[[ -f "$cfg" ]] || { echo "Anti-cheat config not found: $cfg" >&2; exit 1; }

process_pattern='WRFrontiers-Win64-Shipping\.exe|WarRobotsSteamLoader\.exe|MGL\.exe.*(fromsteam|MY.GAMES)'
if pgrep -u "$(id -u)" -f -- "$process_pattern" >/dev/null; then
    echo "WRF or its launcher is already running. Exit it before tracing a clean boundary." >&2
    exit 2
fi

stamp="$(date +%Y%m%d-%H%M%S)"
output_dir="$output_root/$stamp-$route"
mkdir -p -- "$output_dir"
start_lines="$(wc -l < "$launcher_log")"
start_epoch="$(date +%s)"
deadline=$((start_epoch + timeout_seconds))
trace_pid=0
launch_pid=0
autoplay_pid=0
autoplay_status="not-used"
launchmode_probe_pid=0
launchmode_probe_status="not-used"
mgl_pid=""
ach="unknown"
launch_mode="not-seen"
reason="timeout"

stop_trace() {
    if (( trace_pid )) && kill -0 "$trace_pid" 2>/dev/null; then
        kill -INT "$trace_pid" 2>/dev/null || true
        wait "$trace_pid" 2>/dev/null || true
    fi
    trace_pid=0
}

stop_autoplay() {
    if (( autoplay_pid )) && kill -0 "$autoplay_pid" 2>/dev/null; then
        kill -TERM "$autoplay_pid" 2>/dev/null || true
        wait "$autoplay_pid" 2>/dev/null || true
    fi
    autoplay_pid=0
}

stop_launchmode_probe() {
    if (( launchmode_probe_pid )) && kill -0 "$launchmode_probe_pid" 2>/dev/null; then
        kill -TERM "$launchmode_probe_pid" 2>/dev/null || true
        wait "$launchmode_probe_pid" 2>/dev/null || true
    fi
    launchmode_probe_pid=0
}
trap 'stop_trace; stop_autoplay; stop_launchmode_probe; exit 130' INT TERM
trap 'stop_trace; stop_autoplay; stop_launchmode_probe' EXIT

selected_tool="not-applicable"
if [[ "$route" == steam && -r "$steam_root/config/config.vdf" ]]; then
    selected_tool="$(awk '
        /"CompatToolMapping"/ { in_map=1 }
        in_map && /"1491000"/ { in_app=1; next }
        in_app && /"name"/ {
            line=$0
            sub(/^[[:space:]]*"name"[[:space:]]*"/, "", line)
            sub(/"[[:space:]]*$/, "", line)
            print line
            exit
        }
    ' "$steam_root/config/config.vdf")"
    selected_tool="${selected_tool:-Steam default}"
elif [[ "$route" == native ]]; then
    selected_tool="$native_tool"
fi

{
    printf 'route=%s\nstarted=%s\nselected_tool=%s\n' \
        "$route" "$(date --date="@$start_epoch" --iso-8601=seconds)" "$selected_tool"
    printf 'launcher_sha256=%s\n' "$(sha256sum "$launcher" | cut -d' ' -f1)"
    printf 'aclaunchapi64_sha256=%s\n' "$(sha256sum "$acl" | cut -d' ' -f1)"
    printf 'acclient_cfg_sha256=%s\n' "$(sha256sum "$cfg" | cut -d' ' -f1)"
} > "$output_dir/result.txt"

for section in .text .rdata .data .pdata .rsrc .reloc; do
    printf '%s  %s\n' \
        "$(objdump -s -j "$section" "$acl" 2>/dev/null |
            sed -n '/Contents of section/,$p' | sha256sum | cut -d' ' -f1)" \
        "$section"
done > "$output_dir/aclaunchapi64-sections.sha256"

sed -nE 's/^[[:space:]]*<(defaultLaunchMode|host|port|secure)>([^<]*)<\/\1>[[:space:]]*$/\1=\2/p' \
    "$cfg" > "$output_dir/acclient-config-allowlist.txt"

printf 'Launching %s route with %s\n' "$route" "$selected_tool"
"${launch[@]}" >/dev/null 2>&1 &
launch_pid=$!

find_main_mgl() {
    local pid cmd fallback=""
    while read -r pid; do
        [[ -r "/proc/$pid/cmdline" ]] || continue
        cmd="$(tr '\0' ' ' < "/proc/$pid/cmdline")"
        [[ "$cmd" == *MGL.exe* ]] || continue
        [[ "$cmd" == *"--type="* || "$cmd" == *" -job="* ]] && continue
        fallback="$pid"
    done < <(pgrep -u "$(id -u)" -f 'MGL\.exe' 2>/dev/null || true)
    [[ -n "$fallback" ]] && printf '%s\n' "$fallback"
}

while (( $(date +%s) < deadline )); do
    mgl_pid="$(find_main_mgl || true)"
    [[ -n "$mgl_pid" ]] && break
    sleep 0.1
done

if [[ -n "$mgl_pid" ]]; then
    printf 'Attached metadata trace to launcher PID %s\n' "$mgl_pid"
    "$strace_bin" -ttt -yy -s 128 -e trace=connect \
        -p "$mgl_pid" -o "$output_dir/syscalls.raw" 2> "$output_dir/strace-status.log" &
    trace_pid=$!
    WRF_LAUNCHMODE_PROBE_OUTPUT_ROOT="$output_dir/launchmode-service" \
        "$script_dir/run-wrf-launchmode-service-probe.sh" \
        > "$output_dir/launchmode-probe.log" 2>&1 &
    launchmode_probe_pid=$!
    launchmode_probe_status="running"
    if [[ "$route" == native ]]; then
        "$script_dir/wrf-native-autoplay.py" \
            --launcher-timeout "$timeout_seconds" \
            --game-timeout "$timeout_seconds" \
            > "$output_dir/autoplay.log" 2>&1 &
        autoplay_pid=$!
        autoplay_status="running"
    fi
else
    reason="launcher-not-seen"
fi

while [[ "$reason" == timeout ]] && (( $(date +%s) < deadline )); do
    ach_match="$(tail -n "+$((start_lines + 1))" "$launcher_log" |
        sed -nE 's/.* ACH=([0-9]+)([[:space:]].*)?$/\1/p' | head -1 || true)"
    if [[ -n "$ach_match" ]]; then
        ach="$ach_match"
        printf 'ACH detected: %s\n' "$ach"
        reason="ach-seen"
        break
    fi
    sleep 0.1
done

if [[ "$reason" == ach-seen ]]; then
    inherit_deadline=$(( $(date +%s) + 30 ))
    (( inherit_deadline > deadline )) && inherit_deadline=$deadline
    while (( $(date +%s) < inherit_deadline )); do
        while read -r game_pid; do
            [[ -r "/proc/$game_pid/environ" ]] || continue
            launch_mode="$(tr '\0' '\n' < "/proc/$game_pid/environ" |
                sed -nE 's/^AC_LAUNCHMODE=(0x[0-9A-Fa-f]+)$/\1/p' | head -1)"
            if [[ -n "$launch_mode" ]]; then
                printf 'Game inherited AC_LAUNCHMODE=%s\n' "$launch_mode"
                reason="launch-mode-inherited"
                break 2
            fi
        done < <(pgrep -u "$(id -u)" -f 'WRFrontiers-Win64-Shipping\.exe' 2>/dev/null || true)
        if [[ -r "$proton_log" ]] && (( $(stat -c %Y "$proton_log") >= start_epoch )); then
            launch_mode="$(rg 'WRFACH layer=ntdll op=inherit .*value=L"0x[0-9A-Fa-f]+"' "$proton_log" |
                tail -1 | sed -nE 's/.*value=L"(0x[0-9A-Fa-f]+)".*/\1/p' || true)"
            if [[ -n "$launch_mode" ]]; then
                printf 'Game probe recorded inherited AC_LAUNCHMODE=%s\n' "$launch_mode"
                reason="launch-mode-inherited"
                break
            fi
        fi
        sleep 0.1
    done
    [[ -n "$launch_mode" ]] || launch_mode="not-seen"
fi

stop_trace

if (( autoplay_pid )); then
    if [[ "$reason" == launch-mode-inherited ]]; then
        if wait "$autoplay_pid"; then
            autoplay_status="completed"
        else
            autoplay_status="failed"
        fi
        autoplay_pid=0
    else
        stop_autoplay
        autoplay_status="stopped"
    fi
fi

if (( launchmode_probe_pid )); then
    if [[ "$reason" == launch-mode-inherited ]]; then
        if wait "$launchmode_probe_pid"; then
            launchmode_probe_status="completed"
        else
            launchmode_probe_status="failed"
        fi
        launchmode_probe_pid=0
    else
        stop_launchmode_probe
        launchmode_probe_status="stopped"
    fi
fi

raw_trace="$output_dir/syscalls.raw"
if [[ -e "$raw_trace" ]]; then
    rg -i 'connect\(' "$raw_trace" 2>/dev/null |
        sed "s#$HOME#\$HOME#g" > "$output_dir/syscall-boundary-summary.log" || true
fi

{
    printf 'finished=%s\nach=%s\n' "$(date --iso-8601=seconds)" "$ach"
    printf 'ac_launchmode=%s\nreason=%s\nlauncher_linux_pid=%s\n' \
        "$launch_mode" "$reason" "${mgl_pid:-not-seen}"
    printf 'native_autoplay=%s\n' "$autoplay_status"
    printf 'launchmode_service_probe=%s\n' "$launchmode_probe_status"
    printf 'raw_trace_private=true\nnetwork_payload_captured=false\n'
} >> "$output_dir/result.txt"

echo
sed -n '1,30p' "$output_dir/result.txt"
printf 'Private trace: %s\n' "$output_dir"
printf 'The launched application was left running.\n'
