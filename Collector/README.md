# WRF remote compatibility collector

This collector compares successful Steam Deck sessions with Linux desktop
failures without uploading game logs, credentials, command lines, environment
values, packet bodies, memory dumps, or opaque anti-cheat data.

The SteamOS agent parses locally and uploads only normalized events such as:

- game/agent/runtime versions;
- ACH number when it appears in an approved log source;
- anti-cheat Online/Offline timestamps;
- MRAC call/response/failure occurrence;
- backend close code and timing; and
- structured `gate_result`, `pipe_state`, thread, TLS, and exception metadata
  produced by approved payload-free probes.

The initial agent provides baseline game-log collection. Future signed agent
updates can add the Deck-specific probes after their observer effect is tested.
Baseline and instrumented runs remain separately labelled.

## 1. Publish the GHCR package

The workflow publishes:

```text
ghcr.io/owendb1/wrf-compat-tools-collector:latest
```

The agent update signing key was created locally at:

```text
/secure/path/to/collector-agent-signing-key.pem
```

Only its public key is committed. Add the private key to GitHub Actions without
printing it:

```bash
SIGNING_KEY_PATH=/secure/path/to/collector-agent-signing-key.pem
base64 -w0 "$SIGNING_KEY_PATH" \
  | gh secret set AGENT_SIGNING_KEY_PEM_B64 \
      --repo OwendB1/WRF-Compat-Tools
```

Run **Collector GHCR** manually, push a collector change to `production`, or
push a tag such as `collector-v0.1.0`. If the GHCR package remains private, add
a GitHub container-registry credential in Portainer.

## 2. Prepare the server

On the collector host, create the bind-mounted data directory for the container's
unprivileged UID:

```bash
sudo install -d -m 700 /opt/wrf-collector/data
sudo chown 65532:65532 /opt/wrf-collector/data
```

Generate the administrator token:

```bash
openssl rand -hex 32
```

Keep it private. Each volunteer receives a separate token automatically during
one-time enrollment; those tokens are never shown to the administrator.

In Portainer, create a stack from [compose.yaml](compose.yaml) and set:

```text
COLLECTOR_BIND_ADDRESS=192.0.2.10
COLLECTOR_PUBLIC_URL=https://collect.example.com
COLLECTOR_ADMIN_TOKEN=<generated token>
COLLECTOR_RETENTION_DAYS=30
```

Replace the documentation-only address and domain with the collector host's
LAN address and public HTTPS origin.

`COLLECTOR_INGEST_TOKEN` is no longer used and can be removed from an existing
Portainer stack.

The container publishes `<collector-host>:22222` and stores mode-`0600`, append-only
JSONL run files beneath `/opt/wrf-collector/data`.

Test from the server:

```bash
curl --fail http://192.0.2.10:22222/healthz
```

## 3. Configure Nginx Proxy Manager

Create a proxy host with:

| Setting | Value |
| --- | --- |
| Domain | `collect.example.com` |
| Scheme | `http` |
| Forward hostname/IP | `192.0.2.10` |
| Forward port | `22222` |
| Cache assets | Off |
| Block common exploits | On |
| WebSocket support | Off |

Request an SSL certificate, then enable **Force SSL** and **HTTP/2 Support**.
Do not expose port `22222` through the internet router; external clients use
HTTPS port 443 through NPM.

The service has its own authentication:

- `POST /v1/events`: per-device bearer token;
- `/admin` and `GET /v1/runs*`: HTTP Basic password is the admin token;
- `/`, `/healthz`, and signed agent downloads: public;
- `POST /v1/enroll`: 15-minute, one-time enrollment code.

Check the public route:

```bash
curl --fail https://collect.example.com/healthz
```

## 4. Permit subnet-1 analysis access

Routing/firewall policy must allow:

```text
192.0.2.20 -> 192.0.2.10 TCP/22222
```

It must also allow the NPM host/container to reach that destination. Verify from
the analysis workstation:

```bash
curl --fail http://192.0.2.10:22222/healthz
```

Open the LAN dashboard at `http://192.0.2.10:22222/admin`; use any Basic
username and the admin token as the password. The public dashboard is
`https://collect.example.com/admin` with the same credentials.

## 5. Install on SteamOS

No root access or SteamOS read-only filesystem change is required.

First, the administrator opens `https://collect.example.com/admin`, enters a
device label, and privately sends the resulting one-time code to the volunteer.
The code expires after 15 minutes and works once.

The volunteer visits `https://collect.example.com/` and clicks **Download
SteamOS installer**. In Konsole, from the download directory:

```bash
chmod +x install.sh
./install.sh
```

The installer:

1. explains the exact collection scope and asks for consent;
2. downloads the current agent from the configured collector URL;
3. verifies its SHA-256 and Ed25519 signature;
4. prompts privately for the one-time enrollment code;
5. installs a user service; and
6. starts it immediately.

The agent checks for signed updates at startup and every six hours. It has no
remote command or shell feature. Start WRF normally after installation.

Useful local commands:

```bash
systemctl --user status wrf-collector.service
journalctl --user -u wrf-collector.service -f
systemctl --user restart wrf-collector.service
```

To change the token or label, rerun:

```bash
./install.sh --reconfigure
```

The administrator must create a new enrollment code first. Enrolling the same
device label rotates its token and invalidates the old one.

## 6. Uninstall from SteamOS

```bash
~/.local/lib/wrf-collector/uninstall.sh
```

This stops and disables the user service, removes its binary, token-bearing
configuration, probe spool, service file, and the copied uninstaller. It does
not alter Steam, Proton, WRF, or game logs.

## Server removal

Remove the Portainer stack to stop the service. The retained observations remain
at `/opt/wrf-collector/data`. Delete that exact directory separately only when
the collected study data is no longer required.

## Security properties

- local allow-list parsing; source lines never enter an event;
- fixed event schema and 1 MiB request limit;
- separate hashed token per enrolled device;
- one-time enrollment codes expire after 15 minutes;
- constant-time token checks;
- no client IP or hardware identifier stored;
- unprivileged, read-only container with all Linux capabilities dropped;
- 30-day default retention; and
- Ed25519-verified automatic agent updates.
