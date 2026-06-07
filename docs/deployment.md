# BoxFleet Deployment

This document covers the artifact-based deployment path for the management
server and native node agent.

## Shape

```text
management/server host
  - boxfleet-server
  - embedded /admin Web UI
  - embedded /install.sh node installer
  - bf CLI
  - SQLite database

proxy node
  - boxfleet-agent systemd service
  - boxfleet-sing-box systemd service
  - pulled sing-box config
  - local-only V2Ray API at 127.0.0.1:18082
  - public VLESS Reality proxy port, default 39090/tcp
```

Quota enforcement, rate limits, abuse detection, and subscription URLs are not
implemented yet.

## Release Artifacts

GitHub Actions builds Linux amd64 release artifacts when a `v*` tag is pushed.
Create a release by tagging the commit you want to deploy:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The release workflow uploads:

- `bf-<boxfleet-version>-linux-amd64`
- `boxfleet-server-<boxfleet-version>-linux-amd64`
- `boxfleet-agent-<boxfleet-version>-linux-amd64`
- `sing-box-v1.13.13-linux-amd64` built with `with_v2ray_api`
- `boxfleet-<boxfleet-version>-linux-amd64.tar.gz`
- `SHA256SUMS`

The release workflow builds `sing-box` from pinned upstream tag `v1.13.13` with
the BoxFleet-required tags. The Build Artifacts workflow can also be manually
dispatched for pre-release testing, but server deployments should use GitHub
Releases.

Wait for the release workflow to finish, then download the release on a Linux
amd64 host:

```bash
BOXFLEET_VERSION=v0.1.0
curl -fsSLO "https://github.com/ha0xin/BoxFleet/releases/download/${BOXFLEET_VERSION}/boxfleet-${BOXFLEET_VERSION}-linux-amd64.tar.gz"
curl -fsSLO "https://github.com/ha0xin/BoxFleet/releases/download/${BOXFLEET_VERSION}/SHA256SUMS"
sha256sum -c --ignore-missing SHA256SUMS
tar -xzf "boxfleet-${BOXFLEET_VERSION}-linux-amd64.tar.gz"
```

## Server Install

Install the server-side binaries:

```bash
sudo install -d -m 0755 /opt/boxfleet/bin /opt/boxfleet/server
sudo install -m 0755 "bf-${BOXFLEET_VERSION}-linux-amd64" /opt/boxfleet/bin/bf
sudo install -m 0755 "boxfleet-server-${BOXFLEET_VERSION}-linux-amd64" /opt/boxfleet/bin/boxfleet-server
sudo /opt/boxfleet/bin/bf --db /opt/boxfleet/server/boxfleet.db db init
```

Create a systemd unit for the management server:

```ini
[Unit]
Description=BoxFleet management server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=BOXFLEET_ADMIN_TOKEN=<admin-token>
ExecStart=/opt/boxfleet/bin/boxfleet-server --addr 0.0.0.0:18081 --db /opt/boxfleet/server/boxfleet.db --admin-path-token <path-token>
Restart=always
RestartSec=5s
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
```

Then start it:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now boxfleet-server
curl -fsS http://127.0.0.1:18081/healthz
```

Prefer private-network access or a reverse proxy for the admin UI. The admin API
requires `BOXFLEET_ADMIN_TOKEN` or `--admin-token` by default. Use
`--allow-insecure-admin` only for local development.

## Local Build

```bash
npm --prefix web install
npm --prefix web run build
go test ./...
go build -o dist/deploy/bf ./cmd/bf
go build -o dist/deploy/boxfleet-server ./cmd/boxfleet-server
go build -o dist/deploy/boxfleet-agent ./cmd/boxfleet-agent
```

`npm --prefix web run build` writes generated static assets into
`internal/server/webui/assets/generated`, which are embedded into
`boxfleet-server`. Those generated files are ignored by Git.

## Node Bootstrap

Create a node from the Web UI's Nodes page. The generated modal returns a
`boxfleet-bootstrap:...` string and an install command that downloads
`/install.sh` from the management server. On the node, run the generated
command:

```bash
curl -fsSL https://<server-host>:18081/install.sh -o /tmp/boxfleet-install.sh
sudo sh /tmp/boxfleet-install.sh 'boxfleet-bootstrap:...'
```

The embedded install script downloads
`boxfleet-agent-<boxfleet-version>-linux-amd64` and
`sing-box-v1.13.13-linux-amd64` from the GitHub Release matching the running
server version, verifies checksums when supported by `sha256sum`, installs both
under `/opt/boxfleet/bin`, then runs `boxfleet-agent bootstrap`.

The agent writes `/etc/boxfleet/agent.json`, verifies the installed `sing-box`
has `with_v2ray_api`, installs systemd units, pulls config, and starts
`boxfleet-agent.service`. The bootstrap API still accepts an explicit
`sing_box_url` for custom installs, but the Web UI's default flow relies on the
server install script instead.

## Config Flow

The operator edits nodes, proxies, users, and access grants through `bf` and
the Web UI. Publishing a node config stores a target config version in SQLite:

```bash
bf --db /opt/boxfleet/server/boxfleet.db config publish --node <node-name>
```

The node is not changed through SSH after bootstrap. `boxfleet-agent` polls:

```text
GET /api/node/config
```

When the target version or hash changes, the agent writes a candidate config,
runs `sing-box check`, atomically replaces the active config, restarts
`sing-box`, and reports the result.

## Verify

```bash
curl -fsS http://127.0.0.1:18081/healthz
sudo /opt/boxfleet/bin/bf --db /opt/boxfleet/server/boxfleet.db config status --node <node-name>
systemctl status boxfleet-server boxfleet-agent boxfleet-sing-box --no-pager
```

User-facing node information:

```bash
sudo /opt/boxfleet/bin/bf --db /opt/boxfleet/server/boxfleet.db user node-info <user-name> --node <node-name> --format json
```

Live V2Ray API diagnostics can be run through SSH forwarding:

```bash
ssh -N -L 127.0.0.1:18083:127.0.0.1:18082 <node-ssh-target>
bf stats v2ray --addr 127.0.0.1:18083 --pattern 'user>>><auth-name>>>>traffic>>>' --format json
```

## Logs And Traffic

Traffic:

- `sing-box` exposes V2Ray API on `127.0.0.1:18082`.
- The agent reads user counters and uploads positive deltas.
- The first read after a fresh state file is a baseline and does not create
  usage rows.

Network logs:

- The agent streams `sing-box` journald entries with cursor-based deltas.
- Uploads are split into bounded batches instead of reading all logs into
  memory.
- The server parses known access log shapes into compact structured
  `log_events`.
- Raw network log rows are not retained in normal operation. Keep connection
  activity in structured fields such as node, user, source IP, target host,
  target port, action, count, window start, and window end.
- Structured network events are retained for `network_event_retention_days`
  from the `settings` table. The default is 90 days and can be changed from the
  Network Events page or `/api/admin/settings`.

System logs:

- The agent can upload `boxfleet-agent.service` and `boxfleet-sing-box.service`
  journald entries, but the server currently does not retain system log rows.
- System logs remain separate from structured network events if storage is
  re-enabled later.

## Notes

- The public proxy port must be allowed by the cloud firewall.
- Do not expose `127.0.0.1:18082` publicly.
- Reality requires the node to reach `www.amazon.com:443` for the current
  default handshake.
- Current production path is VLESS Reality with `xtls-rprx-vision`.
- Shadowsocks 2022 and Hysteria2 are present in the data model and CLI create
  commands, but full access issuing, rendering, and client information are not
  the current tested path.
