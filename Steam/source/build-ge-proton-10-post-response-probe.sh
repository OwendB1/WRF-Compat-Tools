#!/usr/bin/env bash
set -euo pipefail

build_name="GE-Proton10-34-WRF-PostResponseProbe"
wine_commit="1729f00e17e879f98f9df1f2bca86bc5d21a65df"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
source_dir="${WRF_GE_PROTON_SOURCE_DIR:-$HOME/Games/wf-frontiers/ge-proton10-34-source}"
build_dir="${WRF_GE_PROTON_BUILD_DIR:-$source_dir/build/build-WRF-TLS}"
wine_obj="${WRF_GE_PROTON_WINE_OBJ:-$build_dir/obj-wine-wrf64}"
base_dir="${WRF_GE_PROTON_BASE_DIR:-$HOME/.local/share/Steam/compatibilitytools.d/GE-Proton10-34-WRF-PreferredBase}"
target_dir="${WRF_GE_PROTON_TARGET_DIR:-$HOME/.local/share/Steam/compatibilitytools.d/$build_name}"
container_image="${WRF_GE_PROTON_CONTAINER_IMAGE:-ghcr.io/open-wine-components/umu-sdk:latest}"
jobs="${WRF_PROTON_JOBS:-$(nproc)}"
patch_file="$script_dir/ntdll-wrf-post-response-probe-ge10.patch"

for command_name in git podman install sha256sum; do
    command -v "$command_name" >/dev/null || {
        echo "Missing required command: $command_name" >&2
        exit 1
    }
done
[[ -d "$source_dir/wine" && -f "$wine_obj/Makefile" ]] || {
    echo "Configured GE-Proton Wine build not found: $wine_obj" >&2
    exit 1
}
[[ "$(git -C "$source_dir/wine" rev-parse HEAD)" == "$wine_commit" ]] || {
    echo "Unexpected Wine source revision in $source_dir/wine" >&2
    exit 1
}
[[ -d "$base_dir/files/lib/wine" ]] || {
    echo "Preferred-base candidate not found: $base_dir" >&2
    exit 1
}
[[ ! -e "$target_dir" ]] || {
    echo "Target already exists: $target_dir" >&2
    exit 1
}

if git -C "$source_dir/wine" apply --check "$patch_file" 2>/dev/null; then
    git -C "$source_dir/wine" apply "$patch_file"
elif ! git -C "$source_dir/wine" apply --reverse --check "$patch_file" 2>/dev/null; then
    echo "Probe patch neither applies nor is already applied." >&2
    exit 1
fi

for relative_path in dlls/ntdll/exception.c dlls/ntdll/loader.c dlls/ntdll/ntdll_misc.h; do
    install -m 0644 "$source_dir/wine/$relative_path" "$build_dir/src-wine/$relative_path"
done

podman run --rm \
    -v "$source_dir:$source_dir" \
    -w "$wine_obj/dlls/ntdll" \
    -v "$HOME/.ccache:$HOME/.ccache" \
    -e "CCACHE_DIR=$HOME/.ccache" \
    "$container_image" make -j"$jobs"

module="$wine_obj/dlls/ntdll/x86_64-windows/ntdll.dll"
[[ -f "$module" ]] || {
    echo "Built module missing: $module" >&2
    exit 1
}
grep -aFq WRFPROBE "$module" || {
    echo "Built module does not contain the WRF probe marker." >&2
    exit 1
}

mkdir -p -- "$(dirname -- "$target_dir")"
stage="$(mktemp -d "$(dirname -- "$target_dir")/.wrf-ge-probe.XXXXXX")"
trap 'rm -r -- "$stage"' EXIT
candidate="$stage/$build_name"
mkdir -- "$candidate"
cp -a -- "$base_dir/." "$candidate/"
install -m 0555 "$module" "$candidate/files/lib/wine/x86_64-windows/ntdll.dll"

read -r base_epoch _ < "$base_dir/version"
printf '%s %s\n' "$base_epoch" "$build_name" > "$candidate/version"
cat > "$candidate/compatibilitytool.vdf" <<EOF
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
cat > "$candidate/WRF-PROVENANCE.txt" <<EOF
Base runtime: GE-Proton10-34-WRF-PreferredBase
Base x86_64-windows ntdll.dll SHA-256: $(sha256sum "$base_dir/files/lib/wine/x86_64-windows/ntdll.dll" | cut -d' ' -f1)
Probe x86_64-windows ntdll.dll SHA-256: $(sha256sum "$module" | cut -d' ' -f1)
Patch: ntdll-wrf-post-response-probe-ge10.patch
Enable with: WINEDEBUG="warn+module,+wrfprobe"
Required preferred-base flag: WRF_PREFER_ACCLIENT_BASE=1
EOF

mv -- "$candidate" "$target_dir"
rmdir -- "$stage"
trap - EXIT

echo "Installed $build_name at $target_dir"
echo "Restart Steam and select $build_name for War Robots: Frontiers."
