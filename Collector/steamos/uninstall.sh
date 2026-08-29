#!/usr/bin/env bash
set -euo pipefail

systemctl --user disable --now wrf-collector.service 2>/dev/null || true

service="$HOME/.config/systemd/user/wrf-collector.service"
config="$HOME/.config/wrf-collector/config.json"
probe="$HOME/.local/state/wrf-collector/probe.jsonl"
platform_probe="$HOME/.local/state/wrf-collector/platform-probe.exe"
platform_probe_result="$platform_probe.result.json"
binary="$HOME/.local/bin/wrf-collector"
uninstaller="$HOME/.local/lib/wrf-collector/uninstall.sh"
engine_ini="$HOME/.local/share/Steam/steamapps/compatdata/1491000/pfx/drive_c/users/steamuser/AppData/Local/WRFrontiers/Saved/Config/Windows/Engine.ini"
engine_created_marker="$HOME/.local/lib/wrf-collector/engine-ini-created"

if [[ -e "$engine_ini" ]]; then
    engine_temporary="$(mktemp "${engine_ini}.XXXXXX")"
    sed '/^; BEGIN WRF COLLECTOR DIAGNOSTICS$/,/^; END WRF COLLECTOR DIAGNOSTICS$/d' "$engine_ini" > "$engine_temporary"
    chmod 600 "$engine_temporary"
    mv -- "$engine_temporary" "$engine_ini"
    if [[ -e "$engine_created_marker" ]] && ! grep -q '[^[:space:]]' "$engine_ini"; then
        rm -f -- "$engine_ini"
    fi
fi

rm -f -- "$service" "$config" "$probe" "$platform_probe" "$platform_probe_result" "$binary" "$uninstaller" "$engine_created_marker"
systemctl --user daemon-reload
systemctl --user reset-failed wrf-collector.service 2>/dev/null || true
rmdir -- "$HOME/.config/wrf-collector" "$HOME/.local/state/wrf-collector" 2>/dev/null || true
rmdir -- "$HOME/.local/lib/wrf-collector" 2>/dev/null || true

echo "WRF collector removed. Its diagnostic logging block was removed from Engine.ini."
