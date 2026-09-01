#!/usr/bin/env bash
set -euo pipefail
umask 077

build_name="GE-Proton11-6-WRF-DeckProfile-v2"
wine_commit="9358696fe9a2261329f4a83aa6a65fd436106154"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
work_root="${WRF_WORK_ROOT:-$HOME/Games/wf-frontiers}"
source_dir="${WRF_GE_PROTON_SOURCE_DIR:-$work_root/ge-proton11-6-source}"
build_dir="${WRF_GE_PROTON_BUILD_DIR:-$source_dir/build/build-WRF-GE11-6}"
wine_obj="${WRF_GE_PROTON_WINE_OBJ:-$build_dir/obj-wine-x86_64}"
base_dir="${WRF_GE_PROTON_BASE_DIR:-$HOME/.local/share/Steam/compatibilitytools.d/GE-Proton11-6-WRF-MRAC}"
target_dir="${WRF_GE_PROTON_TARGET_DIR:-$HOME/.local/share/Steam/compatibilitytools.d/$build_name}"
host_acpi="${WRF_ACPI_TABLES_DIR:-$work_root/acpi-tables}"
patch_file="$script_dir/wrf-deck-profile-ge11.patch"
wrapper="$script_dir/proton-ge11-deck-profile-wrapper.sh"
release_file="$script_dir/steamos-deck-os-release"
jobs="${WRF_PROTON_JOBS:-$(nproc)}"

for command_name in gcc git install make sha256sum uuidgen; do
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
[[ -d "$base_dir/files/lib/wine" && -x "$base_dir/proton" ]] || {
    echo "GE-Proton11-6 MRAC base not found: $base_dir" >&2
    exit 1
}
[[ -r "$host_acpi/TPM2" && "$(stat -c %s "$host_acpi/TPM2")" == 76 ]] || {
    echo "A measured 76-byte host TPM2 ACPI table is required." >&2
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

if git -C "$source_dir/wine" apply --check "$patch_file" 2>/dev/null; then
    git -C "$source_dir/wine" apply "$patch_file"
elif ! git -C "$source_dir/wine" apply --reverse --check "$patch_file" 2>/dev/null; then
    echo "GE11 Deck-profile patch neither applies nor is already applied." >&2
    exit 1
fi

install -m 0644 "$source_dir/wine/dlls/ntdll/unix/system.c" \
    "$build_dir/src-wine/dlls/ntdll/unix/system.c"

make -C "$wine_obj" CC=gcc -j"$jobs" dlls/ntdll/ntdll.so

module="$wine_obj/dlls/ntdll/ntdll.so"
[[ -f "$module" ]] || { echo "Built ntdll.so is missing: $module" >&2; exit 1; }
for marker in WRF_DECK_CPU_PROFILE WINE_DMI_ID_DIR WINE_ACPI_TABLES_DIR WRF_DECK_NATIVE_SECURITY; do
    grep -aFq "$marker" "$module" || { echo "Built ntdll.so lacks $marker" >&2; exit 1; }
done

mkdir -p -- "$(dirname -- "$target_dir")"
stage="$(mktemp -d "$(dirname -- "$target_dir")/.wrf-ge11-deck-profile.XXXXXX")"
trap 'rm -rf -- "$stage"' EXIT
candidate="$stage/$build_name"
mkdir -- "$candidate"
cp -al -- "$base_dir/." "$candidate/"

replacement="$candidate/files/lib/wine/x86_64-unix/ntdll.so.wrf-new"
install -m 0555 "$module" "$replacement"
mv -f -- "$replacement" "$candidate/files/lib/wine/x86_64-unix/ntdll.so"

profile="$candidate/wrf-platform"
acpi_profile="$profile/acpi-deck-control"
dmi_profile="$profile/dmi-deck-control"
mkdir -m 0700 -- "$profile" "$acpi_profile" "$dmi_profile"
install -m 0600 "$host_acpi/TPM2" "$acpi_profile/TPM2"

test_uuid="$(uuidgen | tr '[:upper:]' '[:lower:]')"
machine_id="${test_uuid//-/}"
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
printf '%s' "$machine_id" > "$dmi_profile/machine-id"

target_profile="$target_dir/wrf-platform"
cat > "$candidate/user_settings.py.wrf-new" <<EOF
import os

selected = os.environ.get("WRF_DECK_PROFILE", "full").lower()
features = ({"acpi", "dmi", "cpu", "gpu", "os"} if selected == "full" else
            set() if selected in {"", "none"} else set(filter(None, selected.split(","))))
unknown = features - {"acpi", "dmi", "cpu", "gpu", "os"}
if unknown:
    raise RuntimeError("Unknown WRF_DECK_PROFILE feature: " + ",".join(sorted(unknown)))

user_settings = {
    "WRF_PREFER_ACCLIENT_BASE": "1",
    "WRF_DECK_NATIVE_SECURITY": "1",
    "SteamDeck": "1",
    "SteamEnv": "1",
}
if "acpi" in features:
    user_settings["WINE_ACPI_TABLES_DIR"] = r"$target_profile/acpi-deck-control"
if "dmi" in features:
    user_settings["WINE_DMI_ID_DIR"] = r"$target_profile/dmi-deck-control"
if "cpu" in features:
    user_settings["WINE_CPU_TOPOLOGY"] = "4s:0,1,2,3,4,5,6,7"
    user_settings["WRF_DECK_CPU_PROFILE"] = "aerith"
if "gpu" in features:
    user_settings["DXVK_CONFIG"] = "dxgi.customVendorId = 1002; dxgi.customDeviceId = 163f"
EOF
mv -f -- "$candidate/user_settings.py.wrf-new" "$candidate/user_settings.py"

mv -- "$candidate/proton" "$candidate/proton-real"
install -m 0755 "$wrapper" "$candidate/proton"
install -m 0600 "$release_file" "$profile/os-release-steamdeck"

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

cat > "$candidate/WRF-DECK-PROFILE-PROVENANCE.txt.wrf-new" <<EOF
Base runtime: GE-Proton11-6-WRF-MRAC
Wine source commit: $wine_commit
Profile ntdll.so SHA-256: $(sha256sum "$candidate/files/lib/wine/x86_64-unix/ntdll.so" | cut -d' ' -f1)
Patch: wrf-deck-profile-ge11.patch
Default WRF_DECK_PROFILE: full (acpi,dmi,cpu,gpu,os)
ACPI: local 76-byte TPM2 control; DMAR and IVRS absent
DMI: measured Valve/Aerith/Jupiter fields with local synthetic product/board identities
CPU: Aerith API/SMBIOS identity (family 23, model 144, stepping 2) and four-core/eight-thread topology
GPU: working physical adapter retained; DXGI reports captured 1002:163f IDs
OS: SteamOS 3.8.16 profile mounted read-only inside the Proton process namespace
Security API baseline: DMA Guard 0xc0000003; PCP_EKPUB 0x80090027, matching the authentic Deck
No Deck serial, UUID, EK, private key, signed attestation, or network-payload modification is included.
Unmatched: literal in-process CPUID results, raw Deck SMBIOS/ACPI bytes, firmware identity, and signed Steam/TPM attestation.
EOF
mv -f -- "$candidate/WRF-DECK-PROFILE-PROVENANCE.txt.wrf-new" "$candidate/WRF-DECK-PROFILE-PROVENANCE.txt"

mv -- "$candidate" "$target_dir"
rmdir -- "$stage"
trap - EXIT

echo "Installed $build_name at $target_dir"
echo "Restart Steam, select $build_name, and leave WRF_DECK_PROFILE unset for the full control."
echo "Later ablations accept none or a comma list drawn from: acpi,dmi,cpu,gpu,os"
