#!/usr/bin/env bash
set -euo pipefail
umask 077

build_name="GE-Proton10-34-WRF-DeckCompare"
wine_commit="1729f00e17e879f98f9df1f2bca86bc5d21a65df"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
work_root="${WRF_WORK_ROOT:-$HOME/Games/wf-frontiers}"
source_dir="${WRF_GE_PROTON_SOURCE_DIR:-$work_root/ge-proton10-34-source}"
build_dir="${WRF_GE_PROTON_BUILD_DIR:-$source_dir/build/build-WRF-TLS}"
wine_obj="${WRF_GE_PROTON_WINE_OBJ:-$build_dir/obj-wine-wrf64}"
base_dir="${WRF_GE_PROTON_BASE_DIR:-$HOME/.local/share/Steam/compatibilitytools.d/GE-Proton10-34-WRF-HostSecurity}"
target_dir="${WRF_GE_PROTON_TARGET_DIR:-$HOME/.local/share/Steam/compatibilitytools.d/$build_name}"
host_acpi="${WRF_ACPI_TABLES_DIR:-$work_root/acpi-tables}"
host_tpm_ekpub="${WRF_TPM_EKPUB_FILE:-$work_root/tpm-ekpub.blob}"
patch_file="$script_dir/wrf-deck-profile-ge10.patch"
jobs="${WRF_PROTON_JOBS:-$(nproc)}"

for command_name in git make objcopy install sha256sum uuidgen; do
    command -v "$command_name" >/dev/null || {
        echo "Missing required command: $command_name" >&2
        exit 1
    }
done
[[ -d "$source_dir/wine" && -f "$wine_obj/Makefile" ]] || {
    echo "Configured GE-Proton10 Wine build not found: $wine_obj" >&2
    exit 1
}
[[ "$(git -C "$source_dir/wine" rev-parse HEAD)" == "$wine_commit" ]] || {
    echo "Unexpected Wine source revision in $source_dir/wine" >&2
    exit 1
}
[[ -d "$base_dir/files/lib/wine" ]] || {
    echo "GE-Proton10 host-security base not found: $base_dir" >&2
    exit 1
}
[[ -r "$host_acpi/TPM2" && "$(stat -c %s "$host_acpi/TPM2")" == 76 && -r "$host_tpm_ekpub" ]] || {
    echo "The measured 76-byte host TPM2 table and TPM EK public snapshot are required." >&2
    exit 1
}
[[ ! -e "$target_dir" ]] || {
    echo "Target already exists: $target_dir" >&2
    exit 1
}

if git -C "$source_dir/wine" apply --check "$patch_file" 2>/dev/null; then
    git -C "$source_dir/wine" apply "$patch_file"
elif ! git -C "$source_dir/wine" apply --reverse --check "$patch_file" 2>/dev/null; then
    echo "GE-Proton10 Deck-profile patch neither applies nor is already applied." >&2
    exit 1
fi

install -m 0644 "$source_dir/wine/dlls/ntdll/unix/system.c" \
    "$build_dir/src-wine/dlls/ntdll/unix/system.c"
make -C "$wine_obj/dlls/ntdll" -j"$jobs"

module="$wine_obj/dlls/ntdll/ntdll.so"
[[ -f "$module" ]] || { echo "Built ntdll.so is missing: $module" >&2; exit 1; }
for marker in WRF_PREFER_ACCLIENT_BASE WINE_ACPI_TABLES_DIR WINE_DMI_ID_DIR; do
    grep -aFq "$marker" "$module" || { echo "Built ntdll.so lacks $marker" >&2; exit 1; }
done

mkdir -p -- "$(dirname -- "$target_dir")"
stage="$(mktemp -d "$(dirname -- "$target_dir")/.wrf-ge10-deck-compare.XXXXXX")"
trap 'rm -rf -- "$stage"' EXIT
candidate="$stage/$build_name"
mkdir -- "$candidate"
cp -al -- "$base_dir/." "$candidate/"

replacement="$candidate/files/lib/wine/x86_64-unix/ntdll.so.wrf-new"
objcopy --strip-debug "$module" "$replacement"
chmod --reference="$candidate/files/lib/wine/x86_64-unix/ntdll.so" "$replacement"
mv -f -- "$replacement" "$candidate/files/lib/wine/x86_64-unix/ntdll.so"

profile="$candidate/wrf-platform"
acpi_profile="$profile/acpi-tables"
dmi_profile="$profile/dmi-deck-control"
mkdir -m 0700 -- "$profile" "$acpi_profile" "$dmi_profile"
for table in "$host_acpi"/*; do
    [[ -f "$table" ]] || continue
    case "${table##*/}" in DMAR|IVRS) continue ;; esac
    install -m 0600 "$table" "$acpi_profile/${table##*/}"
done
[[ -r "$acpi_profile/TPM2" && ! -e "$acpi_profile/DMAR" && ! -e "$acpi_profile/IVRS" ]] || {
    echo "Failed to create the Deck-matching ACPI control." >&2
    exit 1
}
install -m 0600 "$host_tpm_ekpub" "$profile/tpm-ekpub.blob"

test_uuid="$(uuidgen | tr '[:upper:]' '[:lower:]')"
serial_seed="$(printf '%s' "$test_uuid" | sha256sum | cut -c1-24)"
write_dmi() { printf '%s\n' "$2" > "$dmi_profile/$1"; }
write_dmi bios_date 08/05/2024
write_dmi bios_vendor Valve
write_dmi bios_version F7A0133
write_dmi product_family Aerith
write_dmi product_name Jupiter
write_dmi product_version 1
write_dmi sys_vendor Valve
write_dmi chassis_type 8
write_dmi chassis_vendor Valve
write_dmi chassis_version 1
write_dmi board_name Jupiter
write_dmi board_vendor Valve
write_dmi product_serial "WRF-TEST-${serial_seed:0:12}"
write_dmi board_serial "WRF-TEST-${serial_seed:12:12}"
write_dmi product_uuid "$test_uuid"

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

target_profile="$target_dir/wrf-platform"
tpm_windows="Z:${target_profile//\//\\}\\tpm-ekpub.blob"
cat > "$candidate/user_settings.py.wrf-new" <<EOF
import os

stage = os.environ.get("WRF_DECK_COMPARE_STAGE", "acpi").lower()
user_settings = {
    "WRF_PREFER_ACCLIENT_BASE": "1",
    "WINE_ACPI_TABLES_DIR": r"$target_profile/acpi-tables",
    "WINE_TPM_EKPUB_FILE": r"$tpm_windows",
}
if stage in ("dmi", "full"):
    user_settings["WINE_DMI_ID_DIR"] = r"$target_profile/dmi-deck-control"
if stage == "full":
    user_settings["WINE_CPU_TOPOLOGY"] = "8"
EOF
mv -f -- "$candidate/user_settings.py.wrf-new" "$candidate/user_settings.py"

cat > "$candidate/WRF-DECK-COMPARE-PROVENANCE.txt.wrf-new" <<EOF
Base runtime: GE-Proton10-34-WRF-HostSecurity
Wine commit: $wine_commit
Comparison ntdll.so SHA-256: $(sha256sum "$candidate/files/lib/wine/x86_64-unix/ntdll.so" | cut -d' ' -f1)
Patch: wrf-deck-profile-ge10.patch
Required route: ACH 118 with preferred-base Gate0018 request generation and RPC id 47
Default stage: acpi (authentic TPM; DMAR and IVRS absent)
Optional stages: dmi (measured model fields plus local test identity), full (dmi plus 8 logical CPUs)
No Deck serial, TPM private key, or network-payload modification is included.
EOF
mv -f -- "$candidate/WRF-DECK-COMPARE-PROVENANCE.txt.wrf-new" "$candidate/WRF-DECK-COMPARE-PROVENANCE.txt"

mv -- "$candidate" "$target_dir"
rmdir -- "$stage"
trap - EXIT

echo "Installed $build_name at $target_dir"
echo "Restart Steam and select $build_name. First verify ACH 118 and RPC id 47 on the default acpi stage."
echo "Only then set WRF_DECK_COMPARE_STAGE=dmi or WRF_DECK_COMPARE_STAGE=full."
