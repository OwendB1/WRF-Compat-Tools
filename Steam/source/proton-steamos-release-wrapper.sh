#!/usr/bin/env bash
set -euo pipefail

runtime_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
release_file="$runtime_dir/wrf-platform/os-release-steamdeck"
bwrap=/run/host/usr/bin/bwrap

[[ -x "$bwrap" && -r "$release_file" ]] || {
    echo "SteamOS process profile is unavailable inside Pressure Vessel." >&2
    exit 1
}

exec "$bwrap" \
    --bind / / \
    --dev-bind /dev /dev \
    --proc /proc \
    --ro-bind "$release_file" /etc/os-release \
    --ro-bind "$release_file" /usr/lib/os-release \
    -- "$runtime_dir/proton-real" "$@"
