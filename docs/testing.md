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
- `web/src/**/*.test.*`: API parsing, query serialization, hooks and mutation
  ordering under Vitest.
- `web/e2e`: real server plus Vite, covering resource lifecycle, Mihomo workflow,
  mobile navigation, overflow and browser console regressions.

Random secrets are asserted by shape and constraints, not exact values.
Renderer output is compared structurally, not by whitespace.

Vitest runs without `globals`, so Testing Library's automatic cleanup is not
registered; component tests that render more than once must call
`afterEach(cleanup)`. ECharts needs a canvas and `ResizeObserver`, so chart
success paths are covered by Playwright and by unit tests over the exported pure
projection helpers, not by jsdom renders.

## Pinned invariants

Three families of test exist to catch silent regressions rather than to describe
behavior. Treat a failure as a real signal before adjusting the assertion.

- **Log-parser golden fixtures.** `internal/server/db/log_events_parse_test.go`
  replays `testdata/singbox_logs/*.input.txt` through `parseSingBoxLogEvent` and
  diffs the normalized result against the `.golden.txt` sibling. Network events
  are scraped from unstructured journal text, so this is the only guard that an
  upstream log-format change does not silently drop every event. Regenerate with:

  ```bash
  go test ./internal/server/db -run TestParseSingBoxLogEventGoldenFixtures \
    -update-singbox-log-golden
  ```

  A golden diff after a `SING_BOX_REVISION` bump is a genuine regression, not
  noise to be regenerated away. Rationale is in
  [ADR 0001](adr/0001-network-event-telemetry-source.md).

- **Query-plan assertions.** `TestManagementReadPathsUseBoundedIndexes` plus the
  per-feature EXPLAIN checks in `traffic_series_test.go` and
  `network_event_series_test.go` pin that every bucketed read uses a bounded
  index and never scans `traffic_usage_deltas` or `log_events`. Adding an index
  can move a chosen plan even when no query changed, so run these after any
  migration.

- **Bucket-key pinning.** `TestBucketExprMatchesGoBucketStarts` executes the SQL
  grouping expression in real SQLite and asserts each key equals the Go
  `BucketKey(BucketStart(...))` for the same instant, across fractional seconds
  and every offset. If SQL and Go ever disagree, zero-fill silently blanks every
  bucket instead of failing.

- **Catalog dataset.** `TestEmbeddedCatalog` parses the committed
  `internal/servicecatalog/data/services.tsv` and asserts every rule classifies
  to a non-empty label and category, so a bad regeneration cannot ship blank UI
  groups. Run `go test ./internal/servicecatalog/...` after any dataset bump.

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
