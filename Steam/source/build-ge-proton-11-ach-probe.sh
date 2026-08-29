#!/usr/bin/env bash
set -euo pipefail

build_name="GE-Proton11-6-WRF-ACHProbe"
wine_commit="9358696fe9a2261329f4a83aa6a65fd436106154"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
source_dir="${WRF_GE_PROTON_SOURCE_DIR:-/home/owendb/Games/wf-frontiers/ge-proton11-6-source}"
build_dir="${WRF_GE_PROTON_BUILD_DIR:-$source_dir/build/build-WRF-GE11-6}"
wine_obj="${WRF_GE_PROTON_WINE_OBJ:-$build_dir/obj-wine-x86_64}"
base_dir="${WRF_GE_PROTON_BASE_DIR:-$HOME/.local/share/Steam/compatibilitytools.d/GE-Proton11-6-WRF-PreferredBase}"
target_dir="${WRF_GE_PROTON_TARGET_DIR:-$HOME/.local/share/Steam/compatibilitytools.d/$build_name}"
container_image="${WRF_GE_PROTON_CONTAINER_IMAGE:-ghcr.io/open-wine-components/umu-sdk:latest}"
jobs="${WRF_PROTON_JOBS:-$(nproc)}"
patch_files=(
    "$script_dir/kernelbase-wrf-ach-launchmode-probe-ge11.patch"
    "$script_dir/ntdll-wrf-ach-launchmode-probe-ge11.patch"
)

for command_name in find git install podman sha256sum touch; do
    command -v "$command_name" >/dev/null || {
        echo "Missing required command: $command_name" >&2
        exit 1
    }
done
[[ -d "$source_dir/wine" && -f "$wine_obj/Makefile" ]] || {
    echo "Configured GE-Proton11-6 Wine build not found: $wine_obj" >&2
    exit 1
}
[[ "$(git -C "$source_dir/wine" rev-parse HEAD)" == "$wine_commit" ]] || {
    echo "Unexpected Wine source revision in $source_dir/wine" >&2
    exit 1
}
[[ -d "$base_dir/files/lib/wine" ]] || {
    echo "GE-Proton11-6 preferred-base candidate not found: $base_dir" >&2
    exit 1
}
[[ ! -e "$target_dir" ]] || {
    echo "Target already exists: $target_dir" >&2
    exit 1
}

for patch_file in "${patch_files[@]}"; do
    if git -C "$source_dir/wine" apply --check "$patch_file" 2>/dev/null; then
        git -C "$source_dir/wine" apply "$patch_file"
    elif ! git -C "$source_dir/wine" apply --reverse --check "$patch_file" 2>/dev/null; then
        echo "ACH probe patch neither applies nor is already applied: $patch_file" >&2
        exit 1
    fi
done

install -m 0644 "$source_dir/wine/dlls/kernelbase/process.c" \
    "$build_dir/src-wine/dlls/kernelbase/process.c"
install -m 0644 "$source_dir/wine/dlls/ntdll/env.c" \
    "$build_dir/src-wine/dlls/ntdll/env.c"

container_run=(podman run --rm
    -v "$source_dir:$source_dir"
    -v "$HOME/.ccache:$HOME/.ccache"
    -e "CCACHE_DIR=$HOME/.ccache"
    "$container_image")

# The configured tree may contain host helpers built against a newer glibc than
# the pinned SDK. Rebuild the small helper set in the SDK before relinking the
# PE module; this is cheap and makes repeated builds deterministic.
helper_sources=(winebuild winegcc widl wmc wrc)
helper_targets=(
    tools/winebuild/winebuild
    tools/winegcc/winegcc
    tools/widl/widl
    tools/wmc/wmc
    tools/wrc/wrc
)
for helper in "${helper_sources[@]}"; do
    find "$build_dir/src-wine/tools/$helper" \
        -type f \( -name '*.c' -o -name '*.h' -o -name '*.l' -o -name '*.y' \) \
        -exec touch {} +
done
"${container_run[@]}" make -C "$wine_obj" -j"$jobs" "${helper_targets[@]}"

"${container_run[@]}" make -C "$wine_obj/dlls/kernelbase" -j"$jobs"
"${container_run[@]}" make -C "$wine_obj" -j"$jobs" \
    dlls/ntdll/x86_64-windows/ntdll.dll

module="$wine_obj/dlls/kernelbase/x86_64-windows/kernelbase.dll"
ntdll_module="$wine_obj/dlls/ntdll/x86_64-windows/ntdll.dll"
[[ -f "$module" ]] || { echo "Built module missing: $module" >&2; exit 1; }
[[ -f "$ntdll_module" ]] || { echo "Built module missing: $ntdll_module" >&2; exit 1; }
grep -aFq WRFACH "$module" || {
    echo "Built module does not contain the WRFACH marker." >&2
    exit 1
}
grep -aFq 'WRFACH layer=ntdll' "$ntdll_module" || {
    echo "Built ntdll module does not contain the WRFACH marker." >&2
    exit 1
}

mkdir -p -- "$(dirname -- "$target_dir")"
stage="$(mktemp -d "$(dirname -- "$target_dir")/.wrf-ach-probe.XXXXXX")"
trap 'rm -r -- "$stage"' EXIT
candidate="$stage/$build_name"
mkdir -- "$candidate"
cp -a -- "$base_dir/." "$candidate/"
install -m 0555 "$module" "$candidate/files/lib/wine/x86_64-windows/kernelbase.dll"
install -m 0555 "$module" "$candidate/files/share/default_pfx/drive_c/windows/system32/kernelbase.dll"
install -m 0555 "$ntdll_module" "$candidate/files/lib/wine/x86_64-windows/ntdll.dll"
install -m 0555 "$ntdll_module" "$candidate/files/share/default_pfx/drive_c/windows/system32/ntdll.dll"

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
Base runtime: GE-Proton11-6-WRF-PreferredBase
Base x86_64-windows kernelbase.dll SHA-256: $(sha256sum "$base_dir/files/lib/wine/x86_64-windows/kernelbase.dll" | cut -d' ' -f1)
Probe x86_64-windows kernelbase.dll SHA-256: $(sha256sum "$module" | cut -d' ' -f1)
Base x86_64-windows ntdll.dll SHA-256: $(sha256sum "$base_dir/files/lib/wine/x86_64-windows/ntdll.dll" | cut -d' ' -f1)
Patch: kernelbase-wrf-ach-launchmode-probe-ge11.patch
Patch: ntdll-wrf-ach-launchmode-probe-ge11.patch
Probe x86_64-windows ntdll.dll SHA-256: $(sha256sum "$ntdll_module" | cut -d' ' -f1)
Enable with: WINEDEBUG="warn+module,+wrfach"
Required preferred-base flag: WRF_PREFER_ACCLIENT_BASE=1
EOF

mv -- "$candidate" "$target_dir"
rmdir -- "$stage"
trap - EXIT

echo "Installed $build_name at $target_dir"
echo "Restart Steam, select $build_name, and enable WINEDEBUG=warn+module,+wrfach."
