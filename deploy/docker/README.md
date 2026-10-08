# Docker development and server deployment

The central server is `bfs`. These images never install anything on edge nodes.
The development image includes Go, Node, and a C compiler for SQLite CGO.
Toolchain defaults are defined in the root `Dockerfile`. The host needs Docker
and Compose; host Go/Node installations are optional. Builds can run on the
development machine or a remote Docker host. Production runs in a separate
runtime container with a persistent database; development never mounts
production data.

## Development

Run the commands below from the repository root on a host with Docker and
Compose. For remote development, keep a Git checkout on that host and use Git
to fetch/pull committed changes. Uncommitted local changes must be
transferred explicitly; they do not automatically synchronize. Never overwrite
remote edits or copy `.git`, dependencies, generated assets, databases or secrets.
The development container bind-mounts this checkout; production runs a built
image and does not mount source. Editing source never updates production.

```sh
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

On the Docker host, open `http://127.0.0.1:5173`. For a remote host, forward
the loopback-only ports over SSH (replace `<user>@<host>`):

```sh
ssh -N -L 18082:127.0.0.1:18082 -L 5173:127.0.0.1:5173 '<user>@<host>'
```

Open `http://127.0.0.1:5173`. The runtime service uses port 18081; development
uses 18082 on the host to avoid a collision.

## Build the runtime image

Use a unique tag identifying the source commit and any local changes. Do not
mislabel a locally changed build as a released tag. Set `AGENT_VERSION` to the
agent revision in `.github/workflows/artifacts.yml`; server builds do not change
agent or sing-box versions.

```sh
# Set VERSION to the source release or development build identity,
# and AGENT_VERSION to AGENT_REVISION in the artifact workflow.
: "${VERSION:?Set VERSION}"
: "${AGENT_VERSION:?Set AGENT_VERSION}"
docker build --build-arg VERSION="$VERSION" \
  --build-arg AGENT_VERSION="$AGENT_VERSION" \
  -t "boxfleet-bfs:$VERSION" .
docker run --rm "boxfleet-bfs:$VERSION" --version
```

A development build is not a published release. The current installer downloads
node artifacts from the GitHub release named by server VERSION, and the update manifest requires a matching valid semantic version.
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
   URLs. Configure HTTPS ingress to the production server; the supplied Compose
   file publishes bfs only on loopback. If using Cloudflare Tunnel, run
   cloudflared on the host with origin `http://127.0.0.1:18081`. Do not connect
   production ingress to the development or smoke-test server.
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
   # Set BOXFLEET_IMAGE to the verified runtime image tag or digest.
   : "${BOXFLEET_IMAGE:?Set BOXFLEET_IMAGE}"
   BOXFLEET_IMAGE="$BOXFLEET_IMAGE" \
     docker compose -p boxfleet -f deploy/docker/compose.server.yml up -d
   ```

7. Verify health, hidden-prefix admin authentication, UI, subscription output,
   artifact downloads, node heartbeats and traffic ingestion. Switch ingress
   only after the new server passes local checks; verify externally afterward.
8. Rollback before new writes can restore the source service and the old ingress directly.
   After the target server accepts new writes, database reconciliation is
   required; do not silently revert to an older DB and lose traffic or
   administrative changes.

Do not run two bfs instances against the same database or mount the production
DB into the development container. Keep backups outside the container; container
replacement preserves the bind-mounted DB but is not itself a backup.

## Package the currently deployed binary

For a host migration without a source upgrade, preserve the running release
identity by packaging the verified deployed bfs binary:

```sh
# RUNTIME_CONTEXT is a directory containing the verified bfs binary.
# BOXFLEET_IMAGE identifies the packaged release.
: "${RUNTIME_CONTEXT:?Set RUNTIME_CONTEXT}"
: "${BOXFLEET_IMAGE:?Set BOXFLEET_IMAGE}"
docker build -f deploy/docker/Dockerfile.runtime \
  -t "$BOXFLEET_IMAGE" "$RUNTIME_CONTEXT"
```

The context contains only `bfs`, never database files or server.env. Compare its
SHA256 against the source binary before starting. Production Compose files can
be copied to `/opt/boxfleet/deploy` so service operations are independent of the
development checkout. Set `BOXFLEET_IMAGE` there to the verified runtime tag.

## Operations and verification

Run from the repository root, or use the Compose definition copied into the
deployment directory. Set `BOXFLEET_IMAGE` through the environment or a Compose
`.env` file to the verified runtime image tag or digest.

```sh
docker compose -p boxfleet -f deploy/docker/compose.server.yml ps
docker compose -p boxfleet -f deploy/docker/compose.server.yml logs --tail=50 bfs
curl -fsS http://127.0.0.1:18081/healthz
```

Verify both the container health and the public HTTPS endpoint. If the local
health check passes but public requests return HTTP 502, check the ingress
routing and origin address. For a host-managed Cloudflare Tunnel, the supplied
Compose origin is `http://127.0.0.1:18081`. A proxy in another container needs
network access to bfs; its own loopback address does not reach the host.

After deployment, verify the configured admin mount, authenticated release
endpoint, subscriptions, node heartbeats, and new traffic ingestion. Keep
hostnames, deployed image digests, backup locations, and migration verification
results in the operator's deployment records.
