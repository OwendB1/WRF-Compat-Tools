#!/usr/bin/env bash
set -euo pipefail
umask 077

collector_url="https://collect.example.com"
enrollment_code=""
reconfigure=0
accept_collection=0

while (($#)); do
    case "$1" in
        --url)
            collector_url="${2:?--url requires a value}"
            shift 2
            ;;
        --code)
            enrollment_code="${2:?--code requires a value}"
            shift 2
            ;;
        --reconfigure)
            reconfigure=1
            shift
            ;;
        --accept)
            accept_collection=1
            shift
            ;;
        *)
            echo "Unknown option: $1" >&2
            exit 2
            ;;
    esac
done

[[ "$collector_url" =~ ^https://[A-Za-z0-9.-]+(:[0-9]+)?$ ]] || {
    echo "Collector URL must be HTTPS without a path or credentials." >&2
    exit 1
}

for command_name in awk base64 curl grep head install openssl sed sha256sum systemctl; do
    command -v "$command_name" >/dev/null || {
        echo "Missing required command: $command_name" >&2
        exit 1
    }
done

echo "This permanently enables the complete WRF diagnostic profile and installs a diagnostic user service."
echo "It uploads normalized lifecycle, RPC, transport, liveness, runtime, process, hash, size, state, safe platform-profile, and approved Proton-probe events including exception access types and fault target addresses, plus complete Base64 MRAC ClientRequest request and response blobs."
echo "The platform profile includes non-unique DMI, SMBIOS-shape, PCI, CPU, storage, firmware, and device metadata. While the game is stopped, a signed status-only helper also records the configured Proton runtime's DMA Guard and PCP_EKPUB return codes, output lengths, policy bit, and public-key type/bit length."
echo "Serial values, UUIDs, machine-ID values, raw firmware, TPM EK bytes or hashes, TPM blobs, and private/public key material stay local."
echo "MRAC blobs may contain opaque device or session attestation data. Source logs, credentials, tokens, environment values, command lines, memory dumps, packet captures, and every unrelated RPC body also stay local."
if ((!accept_collection)); then
    read -rp "Continue with collection? [y/N] " consent
    [[ "$consent" =~ ^[Yy]$ ]] || {
        echo "Installation cancelled."
        exit 0
    }
fi

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
install_dir="$HOME/.local/bin"
lib_dir="$HOME/.local/lib/wrf-collector"
config_dir="$HOME/.config/wrf-collector"
state_dir="$HOME/.local/state/wrf-collector"
service_dir="$HOME/.config/systemd/user"
binary="$install_dir/wrf-collector"
config="$config_dir/config.json"
service="$service_dir/wrf-collector.service"
engine_ini="$HOME/.local/share/Steam/steamapps/compatdata/1491000/pfx/drive_c/users/steamuser/AppData/Local/WRFrontiers/Saved/Config/Windows/Engine.ini"
engine_created_marker="$lib_dir/engine-ini-created"
temporary="$(mktemp -d)"
trap 'rm -rf -- "$temporary"' EXIT

curl_args=(curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location --connect-timeout 10 --max-time 120)
public_key="$script_dir/../update-public-key.pem"
if [[ ! -r "$public_key" ]]; then
    public_key="$temporary/update-public-key.pem"
    echo "Downloading update public key..."
    "${curl_args[@]}" "$collector_url/v1/agent/update-public-key.pem" -o "$public_key"
fi
uninstaller="$script_dir/uninstall.sh"
if [[ ! -r "$uninstaller" ]]; then
    uninstaller="$temporary/uninstall.sh"
    echo "Downloading uninstaller..."
    "${curl_args[@]}" "$collector_url/download/uninstall.sh" -o "$uninstaller"
fi

echo "Downloading agent manifest..."
"${curl_args[@]}" "$collector_url/v1/agent/manifest.json" -o "$temporary/manifest.json"
version="$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([A-Za-z0-9._+-]*\)".*/\1/p' "$temporary/manifest.json" | head -n1)"
expected_sha="$(sed -n 's/.*"sha256"[[:space:]]*:[[:space:]]*"\([A-Fa-f0-9]*\)".*/\1/p' "$temporary/manifest.json" | head -n1)"
signature="$(sed -n 's/.*"signature"[[:space:]]*:[[:space:]]*"\([A-Za-z0-9+\/=]*\)".*/\1/p' "$temporary/manifest.json" | head -n1)"
[[ "$version" =~ ^[A-Za-z0-9._+-]{1,64}$ && "$expected_sha" =~ ^[A-Fa-f0-9]{64}$ && -n "$signature" ]] || {
    echo "Invalid update manifest." >&2
    exit 1
}

echo "Downloading collector agent..."
"${curl_args[@]}" "$collector_url/v1/agent/linux-amd64" -o "$temporary/wrf-collector"
printf '%s' "$signature" | base64 -d > "$temporary/signature"
actual_sha="$(sha256sum "$temporary/wrf-collector" | awk '{print $1}')"
[[ "${actual_sha,,}" == "${expected_sha,,}" ]] || {
    echo "Collector checksum verification failed." >&2
    exit 1
}
openssl pkeyutl -verify -pubin -inkey "$public_key" -rawin \
    -in "$temporary/wrf-collector" -sigfile "$temporary/signature" >/dev/null || {
    echo "Collector signature verification failed." >&2
    exit 1
}

mkdir -p -- "$install_dir" "$lib_dir" "$config_dir" "$state_dir" "$service_dir"
chmod 700 "$config_dir" "$state_dir"
install -m 0755 "$temporary/wrf-collector" "$binary"
install -m 0755 "$uninstaller" "$lib_dir/uninstall.sh"

if ((reconfigure)) || [[ ! -e "$config" ]]; then
    if [[ -n "${WRF_COLLECTOR_ENROLLMENT_CODE:-}" ]]; then
        enrollment_code="$WRF_COLLECTOR_ENROLLMENT_CODE"
    elif [[ -z "$enrollment_code" ]]; then
        read -rp "One-time enrollment code: " enrollment_code
        echo
    fi
    [[ "$enrollment_code" =~ ^[A-Fa-f0-9]{32}$ ]] || {
        echo "Enrollment code must be 32 hexadecimal characters." >&2
        exit 1
    }
    enrollment_code="${enrollment_code,,}"
    echo "Enrolling device..."
    if ! printf '{"code":"%s"}' "$enrollment_code" | \
        "${curl_args[@]}" -H 'Content-Type: application/json' --data-binary @- \
            "$collector_url/v1/enroll" -o "$temporary/enrollment.json"; then
        echo "Enrollment failed. Ask the administrator for a new code." >&2
        exit 1
    fi
    ingest_token="$(sed -n 's/.*"token"[[:space:]]*:[[:space:]]*"\([A-Fa-f0-9]*\)".*/\1/p' "$temporary/enrollment.json" | head -n1)"
    device_label="$(sed -n 's/.*"device_label"[[:space:]]*:[[:space:]]*"\([A-Za-z0-9._-]*\)".*/\1/p' "$temporary/enrollment.json" | head -n1)"
    [[ "$ingest_token" =~ ^[A-Fa-f0-9]{64}$ && "$device_label" =~ ^[A-Za-z0-9._-]{1,32}$ ]] || {
        echo "Collector returned an invalid enrollment response." >&2
        exit 1
    }
    log_path="$HOME/.local/share/Steam/steamapps/compatdata/1491000/pfx/drive_c/users/steamuser/AppData/Local/WRFrontiers/Saved/Logs/WRFrontiers.log"
    launcher_log_path="$HOME/.local/share/Steam/steamapps/common/WRFrontiers/MGLauncher/main.log"
    probe_path="$state_dir/probe.jsonl"
    proton_log_path="$HOME/steam-1491000.log"
    printf '{"url":"%s","token":"%s","device_label":"%s","mode":"steamdeck_reference","log_path":"%s","launcher_log_path":"%s","probe_path":"%s","proton_log_path":"%s","auto_update":true}\n' \
        "$collector_url" "$ingest_token" "$device_label" "$log_path" "$launcher_log_path" "$probe_path" "$proton_log_path" > "$config"
    chmod 600 "$config"
fi

sed -i -E 's/"mode":"(baseline|instrumented)"/"mode":"steamdeck_reference"/' "$config"
mkdir -p -- "$(dirname -- "$engine_ini")"
if [[ ! -e "$engine_ini" ]]; then
    install -m 0600 /dev/null "$engine_ini"
    install -m 0600 /dev/null "$engine_created_marker"
fi
engine_temporary="$(mktemp "${engine_ini}.XXXXXX")"
sed '/^; BEGIN WRF COLLECTOR DIAGNOSTICS$/,/^; END WRF COLLECTOR DIAGNOSTICS$/d' "$engine_ini" > "$engine_temporary"
cat >> "$engine_temporary" <<'DIAGNOSTICS'

; BEGIN WRF COLLECTOR DIAGNOSTICS
[Core.Log]
MRAC=VeryVerbose
ACClient=VeryVerbose
GLogBackendRpcWs=VeryVerbose
GLogBackendRpcWsCalls=VeryVerbose
LogBackendRpc=VeryVerbose
GLogBackendRpcProtobuf=VeryVerbose
; END WRF COLLECTOR DIAGNOSTICS
DIAGNOSTICS
chmod 600 "$engine_temporary"
mv -- "$engine_temporary" "$engine_ini"

probe_file="$state_dir/probe.jsonl"
[[ -e "$probe_file" ]] || install -m 0600 /dev/null "$probe_file"

cat > "$service" <<'UNIT'
[Unit]
Description=WRF complete compatibility diagnostics collector
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%h/.local/bin/wrf-collector agent --config %h/.config/wrf-collector/config.json
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=default.target
UNIT
chmod 600 "$service"

systemctl --user daemon-reload
systemctl --user enable wrf-collector.service
systemctl --user restart wrf-collector.service
echo "Installed WRF collector $version. Start WRF normally; the complete diagnostic profile uploads automatically."
echo "Status: systemctl --user status wrf-collector.service"
echo "Uninstall: $lib_dir/uninstall.sh"
