#!/usr/bin/env bash
set -euo pipefail

proton_tag="proton-10.0-4b"
proton_commit="e91ca2be0df2cef4c230cbbc0b86604d73a0bbf6"
wine_commit="b8fdff8e1f855b5276ec4ddca0f31b2792554322"
build_name="Proton-10.0-4b-WRF-TLS"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
source_dir="${WRF_PROTON_SOURCE_DIR:-$HOME/.cache/wrf-proton-10.0-4b}"
base_dir="${WRF_PROTON_BASE_DIR:-$HOME/.local/share/Steam/steamapps/common/Proton 10.0}"
target_dir="${WRF_PROTON_TARGET_DIR:-$HOME/.local/share/Steam/compatibilitytools.d/$build_name}"
jobs="${WRF_PROTON_JOBS:-$(nproc)}"

for command_name in git make podman strip; do
    command -v "$command_name" >/dev/null || {
        echo "Missing required command: $command_name" >&2
        exit 1
    }
done

base_epoch=""
base_version=""
[[ ! -f "$base_dir/version" ]] || read -r base_epoch base_version < "$base_dir/version"
[[ "$base_version" == "$proton_tag" ]] || {
    echo "Required Valve Proton base not found: $base_dir ($proton_tag)" >&2
    exit 1
}
[[ ! -e "$target_dir" ]] || {
    echo "Target already exists: $target_dir" >&2
    exit 1
}

if [[ ! -d "$source_dir/.git" ]]; then
    git clone --branch "$proton_tag" --depth 1 --recurse-submodules \
        https://github.com/ValveSoftware/Proton.git "$source_dir"
fi
[[ "$(git -C "$source_dir" rev-parse HEAD)" == "$proton_commit" ]] || {
    echo "Unexpected Proton source revision in $source_dir" >&2
    exit 1
}
git -C "$source_dir" submodule update --init --recursive
[[ "$(git -C "$source_dir/wine" rev-parse HEAD)" == "$wine_commit" ]] || {
    echo "Unexpected Wine source revision in $source_dir/wine" >&2
    exit 1
}
source_dir="$(git -C "$source_dir" rev-parse --show-toplevel)"

patch_file="$script_dir/ntdll-rebase-stale-tls-pointers.patch"
if git -C "$source_dir/wine" apply --check "$patch_file" 2>/dev/null; then
    git -C "$source_dir/wine" apply "$patch_file"
elif ! git -C "$source_dir/wine" apply --reverse --check "$patch_file" 2>/dev/null; then
    echo "TLS patch neither applies nor is already applied." >&2
    exit 1
fi

make -C "$source_dir" build_name="$build_name" configure
build_dir="$source_dir/build/build-$build_name"
make -C "$build_dir" -j"$jobs" "$build_dir/.wine-wine-requests"
make -C "$source_dir" -j"$jobs" build_name="$build_name" module=ntdll module

module_dir="$source_dir/build/ntdll/lib/wine"
for relative_path in i386-unix/ntdll.so x86_64-unix/ntdll.so; do
    [[ -f "$module_dir/$relative_path" ]] || {
        echo "Built module missing: $module_dir/$relative_path" >&2
        exit 1
    }
done

mkdir -p -- "$(dirname -- "$target_dir")"
stage="$(mktemp -d "$(dirname -- "$target_dir")/.wrf-proton10.XXXXXX")"
trap 'rm -r -- "$stage"' EXIT
candidate="$stage/$build_name"
mkdir -- "$candidate"
cp -a -- "$base_dir/." "$candidate/"
printf '%s %s\n' "$base_epoch" "$build_name" > "$candidate/version"
for relative_path in i386-unix/ntdll.so x86_64-unix/ntdll.so; do
    install -m 0755 "$module_dir/$relative_path" "$candidate/files/lib/wine/$relative_path"
    strip --strip-debug "$candidate/files/lib/wine/$relative_path"
    chmod 0555 "$candidate/files/lib/wine/$relative_path"
done

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
Valve Proton tag: $proton_tag
Valve Proton commit: $proton_commit
Valve Wine commit: $wine_commit
Patch: ntdll-rebase-stale-tls-pointers.patch
EOF

mv -- "$candidate" "$target_dir"
rmdir -- "$stage"
trap - EXIT

echo "Installed $build_name at $target_dir"
echo "Restart Steam, select $build_name, and run the same launch options for the A/B test."
