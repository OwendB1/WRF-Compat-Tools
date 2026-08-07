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
<private-workspace>/collector-agent-signing-key.pem
```

Only its public key is committed. Add the private key to GitHub Actions without
printing it:

```bash
base64 -w0 <private-workspace>/collector-agent-signing-key.pem \
  | gh secret set AGENT_SIGNING_KEY_PEM_B64 \
      --repo OwendB1/WRF-Compat-Tools
```

Run **Collector GHCR** manually, push a collector change to `production`, or
push a tag such as `collector-v0.1.0`. If the GHCR package remains private, add
a GitHub container-registry credential in Portainer.

## 2. Prepare the server

On `192.0.2.10`, create the bind-mounted data directory for the container's
unprivileged UID:

```bash
sudo install -d -o 65532 -g 65532 -m 700 /opt/wrf-collector/data
```

Generate two different tokens:

```bash
openssl rand -hex 32  # COLLECTOR_INGEST_TOKEN
openssl rand -hex 32  # COLLECTOR_ADMIN_TOKEN
```

Keep both private. Give the volunteer only the ingest token.

In Portainer, create a stack from [compose.yaml](compose.yaml) and set:

```text
COLLECTOR_INGEST_TOKEN=<first token>
COLLECTOR_ADMIN_TOKEN=<second token>
COLLECTOR_RETENTION_DAYS=30
```

The container publishes `192.0.2.10:22222` and stores mode-`0600`, append-only
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

- `POST /v1/events`: ingest bearer token;
- `/` and `GET /v1/runs*`: HTTP Basic password is the admin token;
- `/healthz` and signed agent downloads: public.

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
`192.0.2.20`:

```bash
curl --fail http://192.0.2.10:22222/healthz
```

Open the LAN dashboard at `http://192.0.2.10:22222/`; use any Basic username
and the admin token as the password. The public dashboard is
`https://collect.example.com/` with the same credentials.

## 5. Install on SteamOS

No root access or SteamOS read-only filesystem change is required. In Desktop
Mode, open Konsole and fetch only the Collector directory:

```bash
git clone --depth 1 --filter=blob:none --sparse \
  https://github.com/OwendB1/WRF-Compat-Tools.git
cd WRF-Compat-Tools
git sparse-checkout set Collector
./Collector/steamos/install.sh --label volunteer-deck
```

The installer:

1. explains the exact collection scope and asks for consent;
2. downloads the current agent from `https://collect.example.com`;
3. verifies its SHA-256 and Ed25519 signature;
4. prompts privately for the ingest token;
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
./Collector/steamos/install.sh --reconfigure --label volunteer-deck
```

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
- separate ingest and dashboard tokens;
- constant-time token checks;
- no client IP or hardware identifier stored;
- unprivileged, read-only container with all Linux capabilities dropped;
- 30-day default retention; and
- Ed25519-verified automatic agent updates.
