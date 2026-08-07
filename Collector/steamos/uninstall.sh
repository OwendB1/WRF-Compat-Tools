#!/usr/bin/env bash
set -euo pipefail

systemctl --user disable --now wrf-collector.service 2>/dev/null || true

service="$HOME/.config/systemd/user/wrf-collector.service"
config="$HOME/.config/wrf-collector/config.json"
probe="$HOME/.local/state/wrf-collector/probe.jsonl"
binary="$HOME/.local/bin/wrf-collector"
uninstaller="$HOME/.local/lib/wrf-collector/uninstall.sh"

rm -f -- "$service" "$config" "$probe" "$binary" "$uninstaller"
systemctl --user daemon-reload
systemctl --user reset-failed wrf-collector.service 2>/dev/null || true
rmdir -- "$HOME/.config/wrf-collector" "$HOME/.local/state/wrf-collector" 2>/dev/null || true
rmdir -- "$HOME/.local/lib/wrf-collector" 2>/dev/null || true

echo "WRF collector removed. No game, Proton, or Steam files were changed."
