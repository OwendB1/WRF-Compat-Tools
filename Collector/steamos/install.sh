#!/usr/bin/env bash
set -euo pipefail
umask 077

collector_url="https://collect.example.com"
device_label=""
reconfigure=0
accept_collection=0

while (($#)); do
    case "$1" in
        --url)
            collector_url="${2:?--url requires a value}"
            shift 2
            ;;
        --label)
            device_label="${2:?--label requires a value}"
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

for command_name in awk base64 curl head install openssl sed sha256sum systemctl; do
    command -v "$command_name" >/dev/null || {
        echo "Missing required command: $command_name" >&2
        exit 1
    }
done

echo "This installs a user service that uploads normalized WRF compatibility events."
echo "It does not upload source logs, credentials, command lines, packet bodies, or memory dumps."
if ((!accept_collection)); then
    read -rp "Continue with collection? [y/N] " consent
    [[ "$consent" =~ ^[Yy]$ ]] || {
        echo "Installation cancelled."
        exit 0
    }
fi

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
public_key="$script_dir/../update-public-key.pem"
[[ -r "$public_key" ]] || {
    echo "Update public key not found: $public_key" >&2
    exit 1
}

install_dir="$HOME/.local/bin"
lib_dir="$HOME/.local/lib/wrf-collector"
config_dir="$HOME/.config/wrf-collector"
state_dir="$HOME/.local/state/wrf-collector"
service_dir="$HOME/.config/systemd/user"
binary="$install_dir/wrf-collector"
config="$config_dir/config.json"
service="$service_dir/wrf-collector.service"
temporary="$(mktemp -d)"
trap 'rm -rf -- "$temporary"' EXIT

curl_args=(--proto '=https' --tlsv1.2 --fail --silent --show-error --location)
"${curl_args[@]}" "$collector_url/v1/agent/manifest.json" -o "$temporary/manifest.json"
version="$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([A-Za-z0-9._+-]*\)".*/\1/p' "$temporary/manifest.json" | head -n1)"
expected_sha="$(sed -n 's/.*"sha256"[[:space:]]*:[[:space:]]*"\([A-Fa-f0-9]*\)".*/\1/p' "$temporary/manifest.json" | head -n1)"
signature="$(sed -n 's/.*"signature"[[:space:]]*:[[:space:]]*"\([A-Za-z0-9+\/=]*\)".*/\1/p' "$temporary/manifest.json" | head -n1)"
[[ "$version" =~ ^[A-Za-z0-9._+-]{1,64}$ && "$expected_sha" =~ ^[A-Fa-f0-9]{64}$ && -n "$signature" ]] || {
    echo "Invalid update manifest." >&2
    exit 1
}

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
install -m 0755 "$script_dir/uninstall.sh" "$lib_dir/uninstall.sh"

if ((reconfigure)) || [[ ! -e "$config" ]]; then
    if [[ -z "$device_label" ]]; then
        read -rp "Device label [steamdeck-a]: " device_label
        device_label="${device_label:-steamdeck-a}"
    fi
    [[ "$device_label" =~ ^[A-Za-z0-9._-]{1,32}$ ]] || {
        echo "Device label may contain only letters, numbers, dot, underscore, and dash." >&2
        exit 1
    }
    if [[ -n "${WRF_COLLECTOR_TOKEN:-}" ]]; then
        ingest_token="$WRF_COLLECTOR_TOKEN"
    else
        read -rsp "Collector ingest token: " ingest_token
        echo
    fi
    [[ "$ingest_token" =~ ^[A-Fa-f0-9]{32,128}$ ]] || {
        echo "Ingest token must be 32-128 hexadecimal characters." >&2
        exit 1
    }
    log_path="$HOME/.local/share/Steam/steamapps/compatdata/1491000/pfx/drive_c/users/steamuser/AppData/Local/WRFrontiers/Saved/Logs/WRFrontiers.log"
    probe_path="$state_dir/probe.jsonl"
    printf '{"url":"%s","token":"%s","device_label":"%s","mode":"baseline","log_path":"%s","probe_path":"%s","auto_update":true}\n' \
        "$collector_url" "$ingest_token" "$device_label" "$log_path" "$probe_path" > "$config"
    chmod 600 "$config"
fi

probe_file="$state_dir/probe.jsonl"
[[ -e "$probe_file" ]] || install -m 0600 /dev/null "$probe_file"

cat > "$service" <<'UNIT'
[Unit]
Description=WRF payload-free compatibility collector
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
systemctl --user enable --now wrf-collector.service
echo "Installed WRF collector $version. Start WRF normally; normalized events upload automatically."
echo "Status: systemctl --user status wrf-collector.service"
echo "Uninstall: $lib_dir/uninstall.sh"
