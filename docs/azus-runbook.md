# azus Server Runbook

`azus` is the SSH alias for the current BoxFleet management server. It is not
an active proxy node.

## Host Profile

```text
SSH target:       ssh azus
Remote user:      azureuser
Hostname:         uswest2
Platform:         Microsoft Azure VM, x86-64
OS:               Ubuntu 24.04 LTS
CPU:              2 vCPU
Memory:           about 892 MiB
Swap:             none
Root filesystem:  about 61 GiB
```

This is a memory-constrained shared host. Do not build the Web UI, BoxFleet, or
sing-box on it. Build Linux amd64 artifacts in GitHub Actions and deploy only
verified artifacts.

## Runtime Layout

```text
/opt/boxfleet/bin/bf
/opt/boxfleet/bin/boxfleet-server
/opt/boxfleet/artifacts/              node installer artifacts
/opt/boxfleet/server/boxfleet.db      production SQLite database
/opt/boxfleet/backups/                deployment backups
/etc/boxfleet/server.env              admin secrets; never print or copy
/etc/systemd/system/boxfleet-server.service
```

`boxfleet-server` listens on port `18081`. Admin auth and the hidden admin path
come from `/etc/boxfleet/server.env`. Do not expose either value in commands,
logs, documentation, or CI artifacts.

`boxfleet-agent.service` is legacy/inactive on this host. There is no active
`boxfleet-sing-box.service`; proxy traffic belongs on separate node hosts.

## GitHub Actions Deployment

Start from a committed and pushed `main` branch:

```bash
git push origin main
gh workflow run artifacts.yml --ref main
gh run list --workflow artifacts.yml --branch main --limit 3
gh run watch <run-id> --exit-status
```

Download and verify the completed workflow artifact:

```bash
rm -rf /tmp/boxfleet-actions-<run-id>
mkdir -p /tmp/boxfleet-actions-<run-id>
gh run download <run-id> --dir /tmp/boxfleet-actions-<run-id>

cd /tmp/boxfleet-actions-<run-id>/boxfleet-<version>-linux-amd64
shasum -a 256 -c SHA256SUMS
file artifacts/bf-<version>-linux-amd64
file artifacts/boxfleet-server-<version>-linux-amd64
```

Only `bf` and `boxfleet-server` are deployed to this management host. Do not
replace its inactive agent or sing-box binaries as part of a server release.

Upload the two binaries under versioned temporary names:

```bash
scp artifacts/bf-<version>-linux-amd64 \
  azus:/tmp/bf-<version>
scp artifacts/boxfleet-server-<version>-linux-amd64 \
  azus:/tmp/boxfleet-server-<version>
```

Before stopping the service, verify remote checksums against `SHA256SUMS`, run
both binaries with `--help`, and run the new `bf db status` against the
production database. It should show only the expected pending migrations.

## Backup, Replace, And Roll Back

For a release with schema migrations:

1. Create `/opt/boxfleet/backups/pre-<version>-<UTC timestamp>/`.
2. Stop `boxfleet-server`.
3. Copy the current `bf`, `boxfleet-server`, `boxfleet.db`, and any existing
   `boxfleet.db-wal` / `boxfleet.db-shm` into the backup directory.
4. Install the verified new binaries with mode `0755`.
5. Start `boxfleet-server`; startup applies embedded migrations.
6. Verify health and migration status.

For a binary-only replacement at an already-applied schema version, back up and
replace only the two binaries.

The deployment command must use an `ERR` trap. On failure it should stop the new
service, restore the old binaries, restore the database files when migrations
were part of the deployment, and restart the old service.

## Smoke Checks

Run these without printing values from `server.env`:

```bash
ssh azus 'curl -fsS http://127.0.0.1:18081/healthz'
ssh azus 'sudo systemctl is-active boxfleet-server'
ssh azus 'sudo /opt/boxfleet/bin/bf \
  --db /opt/boxfleet/server/boxfleet.db db status'
ssh azus 'sudo journalctl -u boxfleet-server -n 20 --no-pager'
```

Also verify internally that:

- the hidden Admin UI returns HTTP 200;
- an authenticated Admin API request returns HTTP 200;
- `/sub/not-a-valid-token` returns HTTP 404;
- installed binary hashes match the workflow `SHA256SUMS`;
- the startup log reports the expected Actions version.

Never include the admin token, admin path token, node tokens, subscription URLs,
or database contents in deployment output.

