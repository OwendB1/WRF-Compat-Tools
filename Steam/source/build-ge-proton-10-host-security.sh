#!/usr/bin/env bash
set -euo pipefail

build_name="GE-Proton10-34-WRF-HostSecurity"
wine_commit="1729f00e17e879f98f9df1f2bca86bc5d21a65df"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
source_dir="${WRF_GE_PROTON_SOURCE_DIR:-$HOME/Games/wf-frontiers/ge-proton10-34-source}"
build_dir="${WRF_GE_PROTON_BUILD_DIR:-$source_dir/build/build-WRF-TLS}"
wine_obj="${WRF_GE_PROTON_WINE_OBJ:-$build_dir/obj-wine-wrf64}"
base_dir="${WRF_GE_PROTON_BASE_DIR:-$HOME/.local/share/Steam/compatibilitytools.d/GE-Proton10-34-WRF-PostResponseProbe}"
target_dir="${WRF_GE_PROTON_TARGET_DIR:-$HOME/.local/share/Steam/compatibilitytools.d/$build_name}"
acpi_dir="${WRF_ACPI_TABLES_DIR:-$HOME/Games/wf-frontiers/acpi-tables}"
tpm_ekpub="${WRF_TPM_EKPUB_FILE:-$HOME/Games/wf-frontiers/tpm-ekpub.blob}"
container_image="${WRF_GE_PROTON_CONTAINER_IMAGE:-ghcr.io/open-wine-components/umu-sdk:latest}"
jobs="${WRF_PROTON_JOBS:-$(nproc)}"
patch_file="$script_dir/wrf-host-security-apis-ge10.patch"

for command_name in git make podman install sha256sum strings; do
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
    echo "Post-response probe candidate not found: $base_dir" >&2
    exit 1
}
[[ -r "$acpi_dir/TPM2" && -r "$tpm_ekpub" ]] || {
    echo "Readable real-host ACPI and TPM EK public snapshots are required." >&2
    exit 1
}
[[ ! -e "$target_dir" ]] || {
    echo "Target already exists: $target_dir" >&2
    exit 1
}

if git -C "$source_dir/wine" apply --check "$patch_file" 2>/dev/null; then
    git -C "$source_dir/wine" apply "$patch_file"
elif ! git -C "$source_dir/wine" apply --reverse --check "$patch_file" 2>/dev/null; then
    echo "Host-security patch neither applies nor is already applied." >&2
    exit 1
fi

for relative_path in dlls/ntdll/unix/system.c dlls/ncrypt/main.c; do
    install -m 0644 "$source_dir/wine/$relative_path" "$build_dir/src-wine/$relative_path"
done

make -C "$wine_obj/dlls/ntdll" -j"$jobs"

podman run --rm \
    -v "$source_dir:$source_dir" \
    -v "$HOME/.ccache:$HOME/.ccache" \
    -e "CCACHE_DIR=$HOME/.ccache" \
    "$container_image" make -C "$wine_obj/dlls/ncrypt" -j"$jobs"

unix_ntdll="$wine_obj/dlls/ntdll/ntdll.so"
ncrypt_dll="$wine_obj/dlls/ncrypt/x86_64-windows/ncrypt.dll"
[[ -f "$unix_ntdll" && -f "$ncrypt_dll" ]] || {
    echo "Built host-security modules are missing." >&2
    exit 1
}
grep -aFq WINE_ACPI_TABLES_DIR "$unix_ntdll" || {
    echo "Built ntdll lacks ACPI support." >&2
    exit 1
}
strings -el "$ncrypt_dll" | grep -Fq WINE_TPM_EKPUB_FILE || {
    echo "Built ncrypt lacks TPM EK public-key support." >&2
    exit 1
}

mkdir -p -- "$(dirname -- "$target_dir")"
stage="$(mktemp -d "$(dirname -- "$target_dir")/.wrf-ge-host-security.XXXXXX")"
trap 'rm -rf -- "$stage"' EXIT
candidate="$stage/$build_name"
mkdir -- "$candidate"
cp -al -- "$base_dir/." "$candidate/"

install_module() {
    local source="$1" target="$2" replacement="$2.wrf-new"
    cp --reflink=auto -- "$source" "$replacement"
    chmod --reference="$target" "$replacement"
    mv -f -- "$replacement" "$target"
}

install_module "$unix_ntdll" "$candidate/files/lib/wine/x86_64-unix/ntdll.so"
install_module "$ncrypt_dll" "$candidate/files/lib/wine/x86_64-windows/ncrypt.dll"

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

tpm_ekpub_windows="Z:${tpm_ekpub//\//\\}"
cat > "$candidate/user_settings.py" <<EOF
user_settings = {
    "WINE_ACPI_TABLES_DIR": r"$acpi_dir",
    "WINE_TPM_EKPUB_FILE": r"$tpm_ekpub_windows",
}
EOF
cat > "$candidate/WRF-HOST-SECURITY-PROVENANCE.txt" <<EOF
Base runtime: GE-Proton10-34-WRF-PostResponseProbe
Host-security x86_64-unix ntdll.so SHA-256: $(sha256sum "$unix_ntdll" | cut -d' ' -f1)
Host-security x86_64-windows ncrypt.dll SHA-256: $(sha256sum "$ncrypt_dll" | cut -d' ' -f1)
Patch: wrf-host-security-apis-ge10.patch
ACPI source: real-host snapshot selected by WINE_ACPI_TABLES_DIR
TPM source: real-host endorsement public key selected by WINE_TPM_EKPUB_FILE
No private key, fabricated Deck identity, or network-payload modification is included.
EOF

mv -- "$candidate" "$target_dir"
rmdir -- "$stage"
trap - EXIT

echo "Installed $build_name at $target_dir"
echo "Restart Steam, select $build_name, and keep WRF_PREFER_ACCLIENT_BASE=1 enabled."
