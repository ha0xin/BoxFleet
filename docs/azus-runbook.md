# azus Runbook

`azus` is the SSH alias for the production management host, not a proxy node.
It is an Ubuntu x86-64 VM with roughly 1 GiB RAM. Never build the Web UI, Go
binaries, or sing-box there.

## Layout

```text
/opt/boxfleet/bin/{bf,boxfleet-server}
/opt/boxfleet/server/boxfleet.db
/opt/boxfleet/backups/
/etc/boxfleet/server.env
/etc/systemd/system/boxfleet-server.service
```

The server listens on `127.0.0.1:18081` behind its existing ingress. Admin and
path tokens come from `server.env`; never print, copy, or interpolate them into
local logs. Inactive legacy agent/sing-box units on this host are not part of a
server deployment.

## Release preparation

Deploy a completed tagged GitHub Release, not a local build or branch artifact.

```bash
VERSION=vX.Y.Z
gh run list --workflow artifacts.yml --branch "$VERSION" --limit 3
gh release download "$VERSION" --dir /tmp/boxfleet-release
```

`SHA256SUMS` names binaries below `artifacts/`. Individual GitHub downloads are
flat, so either extract the release tarball or reconstruct that directory before
checking every hash. Confirm both server-side candidates are x86-64 Linux
executables.

Upload only:

```text
bf-<server-version>-linux-amd64
boxfleet-server-<server-version>-linux-amd64
```

Before stopping the service, verify their remote SHA256 values, run both with
`--help`, and run the candidate `bf db status` against the production database.

## Replacement and rollback

Use one remote `set -Eeuo pipefail` script with an `ERR` trap.

1. Create `/opt/boxfleet/backups/pre-<version>-<UTC timestamp>/`.
2. Back up current `bf` and `boxfleet-server`.
3. If migrations are pending, also back up DB, WAL, and SHM files while the
   service is stopped.
4. Stop `boxfleet-server`, install candidates with mode `0755`, and start it.
5. Run all smoke checks before removing the trap.

The trap must stop the candidate, restore binaries and any backed-up database
files, then restart the old service. A release with no pending migration is a
binary-only replacement.

## Smoke checks

Without exposing environment values, confirm:

```bash
ssh azus 'curl -fsS http://127.0.0.1:18081/healthz'
ssh azus 'sudo systemctl is-active boxfleet-server'
ssh azus 'sudo /opt/boxfleet/bin/bf --db /opt/boxfleet/server/boxfleet.db db status'
ssh azus 'sudo journalctl -u boxfleet-server -n 30 --no-pager'
```

Also verify:

- installed hashes match the Release;
- startup log contains the expected server version;
- hidden Admin UI and authenticated Admin API return 200;
- `/sub/not-a-valid-token` returns 404;
- `/api/admin/release` reports the intended server, agent, and sing-box targets.

Only status codes and non-secret version fields may be printed. Never output
`server.env`, tokens, subscription URLs, or database contents.
