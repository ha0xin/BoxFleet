# Testing

The release gate is:

```bash
npm ci --prefix web
npm --prefix web run lint
npm --prefix web test
npm --prefix web run build
go test ./...
go vet ./...
npm --prefix web run test:e2e
```

Run sqlc generation first when schema or queries changed. Generated Web assets
must exist before Go tests because the server embeds them.

## Test boundaries

- `internal/server/db`: SQLite facade behavior, constraints, query plans,
  pagination, retention and operation concurrency. Use `t.TempDir()` databases;
  do not test sqlc-generated methods directly.
- `internal/server/api`: handler contracts, authentication, structured errors,
  pagination and fixed update-catalog assets.
- `internal/server/render`: fixed database fixtures and normalized golden JSON.
  Rendering must be deterministic and must not generate credentials.
- `internal/agent`: fake the command runner, service manager, filesystem-facing
  boundaries and HTTP servers. Unit tests must never invoke real `sing-box`,
  `systemctl`, or `journalctl`.
- `internal/cli/bf`: invoke `NewRootCommand` with buffered I/O and a temporary
  database.
- `web/src/**/*.test.*`: API parsing, query serialization, hooks and mutation
  ordering under Vitest.
- `web/e2e`: real server plus Vite, covering resource lifecycle, Mihomo workflow,
  mobile navigation, overflow and browser console regressions.

Random secrets are asserted by shape and constraints, not exact values.
Renderer output is compared structurally, not by whitespace.

## Browser tests

Playwright uses configurable ports:

```bash
BOXFLEET_E2E_SERVER_PORT=18082 npm --prefix web run test:e2e
BOXFLEET_E2E_BROWSERS=chromium,firefox,webkit npm --prefix web run test:e2e
```

The config discovers Chrome on macOS, Linux, and Windows and falls back to
bundled Chromium. `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` may select a specific
binary.

Tests must assert behavior or geometry, not merely take screenshots. Keep the
Network Events page-size URL regression, mobile drawer reachability, bounded
table scrolling, and known console-error checks covered.

## Deployment and performance

Real-node service and sing-box checks belong to the deployment smoke flow, not
the regular suite. Follow [deployment](deployment.md) and the
[azus runbook](azus-runbook.md).

Performance-sensitive releases also follow [performance.md](performance.md).
Query-plan tests protect bounded access paths, while absolute P95 measurements
run against a production-shaped database on the release host.
