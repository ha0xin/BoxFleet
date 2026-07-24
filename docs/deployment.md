# Deployment

BoxFleet deploys prebuilt Linux amd64 artifacts. Do not build on production
hosts. For the current management server, follow the stricter
[azus runbook](azus-runbook.md).

## Releases

Pushing a `v*` tag runs `.github/workflows/artifacts.yml` and publishes:

- `bfs-<server-version>-linux-amd64`
- `boxfleet-agent-<agent-version>-linux-amd64`
- `sing-box-<sing-box-version>-linux-amd64`
- a tarball, `boxfleet-update.json`, and `SHA256SUMS`

Server, agent, and sing-box versions are independent. Change
`AGENT_REVISION` only when agent code or its runtime contract changes, and
`SING_BOX_REVISION` only when the pinned upstream build changes. Server-only
releases therefore do not advertise no-op node upgrades.

```bash
git tag -a vX.Y.Z -m 'BoxFleet vX.Y.Z'
git push origin main vX.Y.Z
gh run list --workflow artifacts.yml --limit 3
gh run watch <run-id> --exit-status
```

Download the release, reconstruct the `artifacts/` layout expected by
`SHA256SUMS` if individual GitHub assets were downloaded flat, and verify every
file before use.

## Management server

The host layout is:

```text
/opt/boxfleet/bin/bfs
/opt/boxfleet/server/boxfleet.db
/opt/boxfleet/backups/
/etc/boxfleet/server.env
```

The service normally runs:

```ini
[Service]
EnvironmentFile=/etc/boxfleet/server.env
ExecStart=/opt/boxfleet/bin/bfs --addr 0.0.0.0:18081 --db /opt/boxfleet/server/boxfleet.db
Restart=always
RestartSec=5s
```

Before replacement:

1. Verify local and remote SHA256 values.
2. Run the server candidate with `--help`.
3. Prepare a backup directory for the server binary and SQLite files.

Stop, replace, restart, and smoke-test inside an `ERR`-trapped script that
backs up the binary plus DB/WAL/SHM files after stopping the service and restores
them on failure. Startup applies embedded migrations. A
server-only release replaces only `bfs`; never update node
components on the management host.

Admin auth is mandatory through `BOXFLEET_ADMIN_TOKEN` unless the explicit
development-only `--allow-insecure-admin` flag is used. A hidden admin prefix
may be configured with `BOXFLEET_ADMIN_PATH_TOKEN`.

## Node bootstrap

Enroll a node in the Web UI and run its generated command on the node:

```bash
curl -fsSL https://<server>/install.sh -o /tmp/boxfleet-install.sh
sudo sh /tmp/boxfleet-install.sh 'boxfleet-bootstrap:...'
```

The server embeds component versions independently. The script downloads the
matching agent and sing-box assets from the server release, verifies both
against `SHA256SUMS`, installs them under `/opt/boxfleet/bin`, and runs
`boxfleet-agent bootstrap`.

Bootstrap writes `/etc/boxfleet/agent.json`, checks for `with_v2ray_api`,
installs systemd units, applies config, and starts the agent. The node begins
`pending`; its first authenticated heartbeat promotes it to `active`.

## Managed updates

Agents claim durable operations through outbound HTTPS. Downloads stream to
same-filesystem partial files, verify size and SHA256, then install under:

```text
/opt/boxfleet/releases/<component>/<version>/
```

Stable paths are atomically switched symlinks. sing-box failures restore the
previous target; an agent update guard restores an agent candidate after three
failed starts. Disabled nodes stay disabled throughout updates.

Existing agents without `operations.v1` need one manual agent installation
before managed updates can reach them. After that, capability names—not one
global protocol number—negotiate features. See [node operations](node-operations.md).

## Verification

After deployment verify without printing secrets:

```bash
curl -fsS http://127.0.0.1:18081/healthz
sudo systemctl is-active boxfleet-server
sudo journalctl -u boxfleet-server -n 30 --no-pager
```

Also confirm:

- installed hashes match the release;
- startup logs report the expected server version;
- hidden Admin UI and authenticated Admin API return 200;
- `/sub/not-a-valid-token` returns 404;
- `/api/admin/release` reports the intended independent component versions.

Node diagnostics:

```bash
systemctl status boxfleet-agent boxfleet-sing-box --no-pager
readlink -f /opt/boxfleet/bin/boxfleet-agent
readlink -f /opt/boxfleet/bin/sing-box
```

Never expose admin/path/node tokens, subscription URLs, environment contents,
or database data in deployment logs.
