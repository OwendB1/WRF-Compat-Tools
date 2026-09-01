#!/usr/bin/env bash
set -euo pipefail

runtime_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
release_file="$runtime_dir/wrf-platform/os-release-steamdeck"
profile="${WRF_DECK_PROFILE:-full}"
echo "WRFDECKPROFILE selected=$profile" >&2

# Make each ablation independent of inherited launch options or the MRAC
# base runtime's former host-security settings.
unset WINE_ACPI_TABLES_DIR WINE_DMI_ID_DIR WINE_CPU_TOPOLOGY WINE_TPM_EKPUB_FILE WRF_DECK_CPU_PROFILE
unset DXVK_FILTER_DEVICE_NAME VKD3D_FILTER_DEVICE_NAME DXVK_CONFIG

case ",$profile," in
    *,os,*|,full,)
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
        ;;
    *)
        exec "$runtime_dir/proton-real" "$@"
        ;;
esac
