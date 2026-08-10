#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
release_file="${WRF_STEAMOS_RELEASE_FILE:-$script_dir/steamos-deck-os-release}"

[[ $# -gt 0 ]] || {
    echo "Usage: $0 COMMAND [ARG ...]" >&2
    exit 2
}
[[ -r "$release_file" ]] || {
    echo "SteamOS release profile is not readable: $release_file" >&2
    exit 1
}
command -v bwrap >/dev/null || {
    echo "Missing required command: bwrap" >&2
    exit 1
}

exec bwrap \
    --bind / / \
    --dev-bind /dev /dev \
    --proc /proc \
    --ro-bind "$release_file" /etc/os-release \
    --ro-bind "$release_file" /usr/lib/os-release \
    -- "$@"
