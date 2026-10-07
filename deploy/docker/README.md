# Docker development and server builds on bero

The central server is `bfs`. These images never install anything on edge nodes.
The Go development image pins Go 1.27.1, with Node 24 and a C compiler for SQLite
CGO. The host needs Docker and Compose; host Go/Node installations are optional.
Remote builds on bero are supported. Production runs in a separate runtime
container with a persistent database; development never mounts production data.

## Development

On bero, `~/Projects/BoxFleet` (`/root/Projects/BoxFleet` for the current SSH
account) is a real Git checkout of `ha0xin/BoxFleet`, branch `codex/cloudflare-ui`.
Use Git to fetch/pull committed changes. Uncommitted local changes must be
transferred explicitly; they do not automatically synchronize. Never overwrite
remote edits or copy `.git`, dependencies, generated assets, databases or secrets.
The development container bind-mounts this checkout; production runs a built
image and does not mount source. Editing source never updates production.

```sh
cd /root/Projects/BoxFleet
docker compose -p boxfleet-dev -f deploy/docker/compose.dev.yml up -d --build
docker compose -p boxfleet-dev -f deploy/docker/compose.dev.yml exec dev bash
# Inside the container:
go version
npm ci --prefix web
npm --prefix web run build
go test ./...
go vet ./...
```

Source is bind-mounted. Go modules, Go build cache, npm cache and web dependencies
use persistent named volumes. `docker compose down` keeps them; `down -v` deletes
them. The development container is root and can edit its mounted source tree.

For a temporary backend with a separate development DB:

```sh
go run ./cmd/bfs --addr 0.0.0.0:18081 --db /tmp/boxfleet-dev.db --admin-token devtoken
# In another container shell, start the UI against this backend:
npm --prefix web run dev:api -- --host 0.0.0.0
```

From your workstation, tunnel the loopback-only ports:

```sh
ssh -N -L 18082:127.0.0.1:18082 -L 5173:127.0.0.1:5173 bero
```

Open `http://127.0.0.1:5173`. The runtime service uses port 18081; development
uses 18082 on the host to avoid a collision.

## Build the runtime image

Use a unique tag identifying the source commit and any local changes. Do not
mislabel a locally changed build as a released tag. Set `AGENT_VERSION` to the
agent revision in `.github/workflows/artifacts.yml`; server builds do not change
agent or sing-box versions.

```sh
docker build --build-arg VERSION=4d590df6cc3f-docker \
  --build-arg AGENT_VERSION=v0.8.1 \
  -t boxfleet-bfs:4d590df6cc3f-docker .
docker run --rm boxfleet-bfs:4d590df6cc3f-docker --version
```

The example is a development image, not a production release. The current
installer downloads node artifacts from the GitHub release named by server
VERSION, and the update manifest requires a matching valid semantic version.
A commit-based VERSION cannot enroll/update nodes through those release paths.
For an unchanged tagged source build, use its actual release VERSION and matching
artifacts. For unreleased source, prepare a real release identity plus manifest
and artifact publication before production cutover; a local artifact mount alone
does not fix the install script's GitHub download URLs.

The multi-stage build builds the embedded UI before compiling bfs. Runtime
contains the server and CA certificates, without the development toolchain.
The process runs as UID/GID 10001 and has an HTTP healthcheck. UI/Go dependencies
are cached by BuildKit. Run required checks before promoting a build; building
an image alone does not run tests. Pin base image digests for release reproducibility.

## Database migration and production startup

1. Preserve the existing public hostname so nodes and subscriptions keep their
   URLs. The operator installs cloudflared manually on bero. Configure its
   Cloudflare Tunnel origin as `http://127.0.0.1:18081`; bfs publishes only on
   loopback. Run cloudflared on the host for this origin address. Do not connect
   the existing production tunnel to the development or smoke-test server.
   Inventory the source host's ingress and served artifacts before cutover.
2. Keep a rollback copy of the source binary, service configuration and DB. Never
   copy a live SQLite DB with plain `cp`/`rsync`; use SQLite backup for a rehearsal.
3. Rehearse startup and migrations on a separate database copy and isolated port.
   Validate integrity, foreign keys, schema, user/node counts and traffic totals.
4. For final cutover, stop the source bfs service, take a consistent final DB snapshot, and
   transfer it plus `/etc/boxfleet/server.env` securely without logging secrets.
   Freeze the old writer until rollback or successful cutover.
5. Place DB at `/opt/boxfleet/server/boxfleet.db`, owned by UID/GID 10001. The
   directory must also be writable for WAL/SHM. Keep server.env mode 0600.
   Copy any locally served node artifacts into `/opt/boxfleet/artifacts`;
   directory contents must be readable by UID 10001. These are not in the image.
   Only set `BOXFLEET_ARTIFACT_DIR=/artifacts` when this directory contains a
   matching, verified release manifest; otherwise retain GitHub release lookup.
6. Start with the immutable build tag:

   ```sh
   BOXFLEET_IMAGE=boxfleet-bfs:4d590df6cc3f-docker \
     docker compose -p boxfleet -f deploy/docker/compose.server.yml up -d
   ```

7. Verify health, hidden-prefix admin authentication, UI, subscription output,
   artifact downloads, node heartbeats and traffic ingestion. Switch ingress
   only after the new server passes local checks; verify externally afterward.
8. Rollback before new writes can restore the source service and the old ingress directly.
   After bero accepts new writes, database reconciliation is required; do not
   silently revert to an older DB and lose traffic or administrative changes.

Do not run two bfs instances against the same database or mount the production
DB into the development container. Keep backups outside the container; container
replacement preserves the bind-mounted DB but is not itself a backup.

## Package the currently deployed binary

For a host migration without a source upgrade, preserve the running release
identity by packaging the verified deployed bfs binary:

```sh
docker build -f deploy/docker/Dockerfile.runtime \
  -t boxfleet-bfs:v0.13.0-rc.6-migration /srv/boxfleet/migration/runtime
```

The context contains only `bfs`, never database files or server.env. Compare its
SHA256 against the source binary before starting. Production Compose files can
be copied to `/opt/boxfleet/deploy` so service operations are independent of the
development checkout. Set `BOXFLEET_IMAGE` there to the verified runtime tag.

cloudflared is installed and managed manually by the operator. Its origin is
`http://127.0.0.1:18081`; retain the existing public hostname.

## Current production deployment

The server on bero runs image `boxfleet-bfs:v0.13.0-rc.6-migration`, preserving
server `v0.13.0-rc.6`, agent `v0.8.0` and sing-box `v1.14.2`. The verified image
ID is `sha256:d92897c1ecfaf3c55822cb153df83f67fe082c1b3ea369d6812012418faebb72`.

```sh
cd /opt/boxfleet/deploy
docker compose -p boxfleet ps
docker compose -p boxfleet logs --tail=50 bfs
curl -fsS http://127.0.0.1:18081/healthz
```

The migration checkpoint backup is
`/opt/boxfleet/backups/pre-docker-migration-20261007/boxfleet.db`; it matches the
stopped source database SHA256
`58fcb610e122fde600705cf4630089fa672b17ce52497c9d98bbd3dea946c72b`.
Verification passed SQLite quick_check, foreign_key_check, schema 29, table counts
and raw/billable traffic totals of 5,846,632,665,794 bytes. Raw verification and
transfer logs are retained under `/srv/boxfleet/migration` on bero.

The public endpoint is `https://boxfleet.122368.xyz`. The operator controls the
Tunnel cutover. A healthy local container with public HTTP 502 means public
routing/origin still needs verification; check the hostname routes to bero's
Tunnel and that its origin is `http://127.0.0.1:18081`. After cutover verify the
existing hidden admin mount, authenticated release endpoint, node heartbeats
and new traffic ingestion. Do not treat local health as a completed cutover.
