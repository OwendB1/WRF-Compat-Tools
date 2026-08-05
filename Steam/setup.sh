#!/usr/bin/env bash
set -euo pipefail

tool_name="GE-Proton10-34-WRF-TLS"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
runtime_dir="$script_dir/runtime"
parts_prefix="$tool_name.tar.zst.part-"

usage() {
    echo "Usage: $0 [--target /path/to/compatibilitytools.d]"
}

if (( $# == 0 )); then
    if [[ -d "$HOME/.local/share/Steam" ]]; then
        target="$HOME/.local/share/Steam/compatibilitytools.d"
    elif [[ -d "$HOME/.steam/root" ]]; then
        target="$HOME/.steam/root/compatibilitytools.d"
    elif [[ -d "$HOME/.var/app/com.valvesoftware.Steam/data/Steam" ]]; then
        target="$HOME/.var/app/com.valvesoftware.Steam/data/Steam/compatibilitytools.d"
    else
        echo "Steam installation not found. Pass --target explicitly." >&2
        exit 1
    fi
elif (( $# == 2 )) && [[ "$1" == "--target" ]]; then
    target="$2"
else
    usage >&2
    exit 2
fi

for command_name in pgrep sha256sum tar zstd; do
    command -v "$command_name" >/dev/null || {
        echo "Required command not found: $command_name" >&2
        exit 1
    }
done

[[ -f "$runtime_dir/SHA256SUMS" ]] || {
    echo "Runtime checksum manifest missing: $runtime_dir/SHA256SUMS" >&2
    exit 1
}

shopt -s nullglob
parts=("$runtime_dir/$parts_prefix"*)
shopt -u nullglob
(( ${#parts[@]} )) || {
    echo "Runtime archive parts missing from $runtime_dir" >&2
    exit 1
}

mkdir -p -- "$target"
install_dir="$target/$tool_name"
[[ ! -e "$install_dir" ]] || {
    echo "Compatibility tool already exists: $install_dir" >&2
    echo "Remove it manually before reinstalling." >&2
    exit 1
}

if pgrep -u "$(id -u)" -x steam >/dev/null; then
    echo "Steam is running. Fully exit Steam to continue; waiting..."
    while pgrep -u "$(id -u)" -x steam >/dev/null; do
        sleep 2
    done
    echo "Steam closed. Continuing installation."
fi

echo "Verifying runtime archive..."
(cd -- "$runtime_dir" && sha256sum --check SHA256SUMS)

stage="$(mktemp -d "$target/.wrf-proton-install.XXXXXX")"
cleanup() {
    [[ ! -e "$stage" ]] || rm -r -- "$stage"
}
trap cleanup EXIT INT TERM

echo "Extracting $tool_name..."
cat -- "${parts[@]}" | zstd --decompress --stdout | tar -xf - -C "$stage"

extracted="$stage/$tool_name"
[[ -x "$extracted/proton" && -f "$extracted/compatibilitytool.vdf" ]] || {
    echo "Extracted runtime is incomplete." >&2
    exit 1
}

expected_ntdll="47e9fddf687792d041b2517e7ec24e7b8c0bc393d65b80261237a7c802fa893e"
actual_ntdll="$(sha256sum "$extracted/files/lib/wine/x86_64-unix/ntdll.so" | awk '{print $1}')"
[[ "$actual_ntdll" == "$expected_ntdll" ]] || {
    echo "Patched ntdll.so verification failed." >&2
    exit 1
}

mv -- "$extracted" "$install_dir"
rmdir -- "$stage"
trap - EXIT INT TERM

echo "Installed: $install_dir"
echo "Restart Steam, select $tool_name, then use these launch options:"
echo 'WINEDLLOVERRIDES="GCLay.dll=d;GCLay64.dll=d" SteamDeck=1 %command%'
