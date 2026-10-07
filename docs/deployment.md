# Deployment

BoxFleet supports prebuilt Linux amd64 artifacts and Docker builds on bero.
Development runs in a Go container on bero from `~/Projects/BoxFleet`.
For Docker development, server images and operations, see the
[Docker runbook](../deploy/docker/README.md).

## Releases

Pushing a `v*` tag runs `.github/workflows/artifacts.yml` and publishes:

- `bfs-<server-version>-linux-amd64`
- `boxfleet-agent-<agent-version>-linux-amd64`
- `sing-box-<sing-box-version>-linux-amd64`
- a tarball, `boxfleet-update.json`, and `SHA256SUMS`

Server, agent, and sing-box version identities and installation choices are
independent, but publication is still bundled under one server release. Change
`AGENT_REVISION` only when agent code or its runtime contract changes, and
`SING_BOX_REVISION` only when the pinned upstream build changes. Server-only
releases therefore do not advertise no-op node upgrades. See
[component compatibility](component-compatibility.md) for the exact boundary,
supported rolling window, and upgrade order.

```bash
git tag -a vX.Y.Z -m 'BoxFleet vX.Y.Z'
git push origin main vX.Y.Z
gh run list --workflow artifacts.yml --limit 3
gh run watch <run-id> --exit-status
```

Download the release, reconstruct the `artifacts/` layout expected by
`SHA256SUMS` if individual GitHub assets were downloaded flat, and verify every
file before use.

## Management server on bero

The production server runs as a Docker container. Development uses a separate
Go container and a real Git checkout at `~/Projects/BoxFleet`; production does
not mount that checkout. See the [Docker runbook](../deploy/docker/README.md).

```text
/root/Projects/BoxFleet                 development Git checkout
/opt/boxfleet/deploy/compose.yml        production Compose definition
/opt/boxfleet/deploy/.env               immutable image selection
/opt/boxfleet/server/boxfleet.db        persistent SQLite database
/opt/boxfleet/artifacts/                optional local node artifacts
/opt/boxfleet/backups/                  rollback snapshots
/etc/boxfleet/server.env               authentication and server configuration
```

```sh
ssh bero
cd /opt/boxfleet/deploy
docker compose -p boxfleet ps
docker compose -p boxfleet logs --tail=50 bfs
curl -fsS http://127.0.0.1:18081/healthz
```

The runtime process uses UID/GID 10001; the database directory and DB must be
writable by it, including SQLite WAL/SHM files. Keep server.env mode 0600 and
outside image build contexts. Admin authentication and the existing hidden
admin prefix remain required.

cloudflared is installed manually on the host. Keep the existing public domain
and configure the Tunnel origin as `http://127.0.0.1:18081`. Docker publishes
that port only on loopback. The development backend uses host port 18082.

Before a runtime upgrade, run the required checks, record the image digest,
compare migrations and preserve the previous image. For schema/data changes,
stop the writer and take a consistent database backup before startup applies
embedded migrations. Never mount a production DB into development or run two
bfs instances against it. Retain the previous rollback snapshot after success;
a failed upgrade must not prune backups. Container recreation is not a backup.

For host migration, stop the source writer, checkpoint SQLite and transfer
source-to-target directly over SSH/rsync. Verify DB SHA256, integrity, foreign
keys, table counts and traffic totals before accepting new reports. Once the
new host accepts writes, rollback requires reconciling its latest data.

Server and node component versions remain independent. A development image
with a commit-based VERSION cannot use the release-bound node installer and
update catalog. Preserve the deployed release identity for a host migration;
prepare matching published artifacts when promoting a new source release.

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
detects systemd or OpenRC, installs the matching services, applies config, and starts the agent. Alpine Linux nodes use OpenRC with `supervise-daemon`; Debian and Ubuntu nodes continue to use systemd. The node begins
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

On Alpine/OpenRC use `rc-service boxfleet-agent status` and
`rc-service boxfleet-sing-box status`. OpenRC service output is retained under
`/opt/boxfleet/log/` and uploaded by the same bounded log-reporting pipeline.

Never expose admin/path/node tokens, subscription URLs, environment contents,
or database data in deployment logs.
