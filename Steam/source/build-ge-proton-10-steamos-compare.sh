#!/usr/bin/env bash
set -euo pipefail
umask 077

build_name="GE-Proton10-34-WRF-DeckCompare-SteamOS"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
base_dir="${WRF_GE_PROTON_BASE_DIR:-$HOME/.local/share/Steam/compatibilitytools.d/GE-Proton10-34-WRF-DeckCompare}"
target_dir="${WRF_GE_PROTON_TARGET_DIR:-$HOME/.local/share/Steam/compatibilitytools.d/$build_name}"
wrapper="$script_dir/proton-steamos-release-wrapper.sh"
release_file="$script_dir/steamos-deck-os-release"

for command_name in install sha256sum; do
    command -v "$command_name" >/dev/null || {
        echo "Missing required command: $command_name" >&2
        exit 1
    }
done
[[ -x "$base_dir/proton" && -d "$base_dir/wrf-platform" ]] || {
    echo "GE-Proton10 Deck-comparison base not found: $base_dir" >&2
    exit 1
}
[[ -x "$wrapper" && -r "$release_file" ]] || {
    echo "SteamOS wrapper inputs are missing." >&2
    exit 1
}
[[ ! -e "$target_dir" ]] || {
    echo "Target already exists: $target_dir" >&2
    exit 1
}

mkdir -p -- "$(dirname -- "$target_dir")"
stage="$(mktemp -d "$(dirname -- "$target_dir")/.wrf-ge10-steamos-compare.XXXXXX")"
trap 'rm -rf -- "$stage"' EXIT
candidate="$stage/$build_name"
mkdir -- "$candidate"
cp -al -- "$base_dir/." "$candidate/"

mv -- "$candidate/proton" "$candidate/proton-real"
install -m 0755 "$wrapper" "$candidate/proton"
install -m 0600 "$release_file" "$candidate/wrf-platform/os-release-steamdeck"

read -r base_epoch _ < "$base_dir/version"
printf '%s %s\n' "$base_epoch" "$build_name" > "$candidate/version.wrf-new"
mv -f -- "$candidate/version.wrf-new" "$candidate/version"
cat > "$candidate/compatibilitytool.vdf.wrf-new" <<EOF
"compatibilitytools"
{
  "compat_tools"
  {
    "$build_name"
    {
      "install_path" "."
      "display_name" "$build_name"
      "from_oslist" "windows"
      "to_oslist" "linux"
    }
  }
}
EOF
mv -f -- "$candidate/compatibilitytool.vdf.wrf-new" "$candidate/compatibilitytool.vdf"

cat > "$candidate/WRF-STEAMOS-COMPARE-PROVENANCE.txt.wrf-new" <<EOF
Base runtime: GE-Proton10-34-WRF-DeckCompare
Base version: $(tr -d '\r\n' < "$base_dir/version")
Proton wrapper SHA-256: $(sha256sum "$candidate/proton" | cut -d' ' -f1)
SteamOS release SHA-256: $(sha256sum "$candidate/wrf-platform/os-release-steamdeck" | cut -d' ' -f1)
Process-visible OS identity: SteamOS 3.8.16 / Holo / steamdeck
The profile is mounted read-only after Pressure Vessel starts and exists only for this Proton process tree.
EOF
mv -f -- "$candidate/WRF-STEAMOS-COMPARE-PROVENANCE.txt.wrf-new" "$candidate/WRF-STEAMOS-COMPARE-PROVENANCE.txt"

mv -- "$candidate" "$target_dir"
rmdir -- "$stage"
trap - EXIT

echo "Installed $build_name at $target_dir"
echo "Select this runtime and keep WRF_DECK_COMPARE_STAGE=full for the cumulative control."
