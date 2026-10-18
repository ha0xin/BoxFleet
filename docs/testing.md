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
- `internal/singboxapi`: the vendored proto contract and the gRPC client, both
  exercised against an in-process fake daemon. See below.
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

The following test families exist to catch silent regressions rather than to describe
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

- **Upstream proto conformance.** `internal/singboxapi/daemonpb` diffs the
  vendored descriptor against
  the upstream descriptor fixture in `daemonpb/testdata/`, a committed copy of the
  real compiled upstream descriptor. See
  [connection telemetry](#connection-telemetry).

- **Default node config shape.** `TestRenderNodeConfigDefaultShapeIsUnchanged`
  and `TestRenderNodeConfigOptOutRestoresDefaultBytes` assert that a node without
  connection telemetry omits the optional service, and opting out restores the
  default bytes. Unsupported installed binaries must remain safe on this path.

## Connection telemetry

Unit tests in this feature run in-process against fake daemons and committed
protocol fixtures. They need no real sing-box binary. Real-binary compatibility
and service checks belong to deployment validation.

- **Fake gRPC daemon.** `internal/singboxapi/client_test.go` and
  `internal/agent/connections_test.go` each stand up a real gRPC server on
  `127.0.0.1:0` and script the connection stream against it. The auth
  interceptor is a faithful copy of upstream's `daemon/server.go` **including
  its empty-secret bypass**: reproducing the real check, rather than a convenient
  approximation, is what makes the credential tests mean anything. A real
  loopback listener is used rather than `bufconn`, matching
  `internal/v2raystats/client_test.go`, so the transport, the metadata
  propagation and the client's loopback validation are all exercised for real.

  `TestEmptyDaemonSecretAcceptsAnything` pins the upstream bypass itself. If
  upstream ever fixes it, that test fails and tells the next person the rationale
  for `ErrEmptySecret` has changed.

- **Proto conformance, mechanically.** A hand-transcribed field table is a second
  thing that can be wrong, silently. Instead the real upstream descriptor is
  committed as a fixture and diffed field by field.
  `TestVendoredFieldsMatchUpstream` fails on any number, name, type or
  cardinality change, and on any upstream field that is not vendored and not in
  the explicit omission allow-list — currently exactly
  `{Connection.14, Connection.15}`. An accidental omission decodes as a zero
  value and loses data quietly, so it must fail as loudly as a wrong field
  number. **These diffs are findings to investigate, not chores to regenerate
  away.** Procedure for moving the pin:
  [`internal/singboxapi/README.md`](../internal/singboxapi/README.md).

- **Query plans.** `TestConnectionTelemetryReadPathsUseBoundedIndexes` holds
  `connection_events` to the same standard as `log_events`: the range read, the
  host ranking, the node/user filters and the retention delete must all use a
  bounded or covering index, never a table scan.

- **Hostile ingest.** `RecordConnectionReport` is tested against the payloads a
  compromised node token could send — negative and near-`int64`-max measures,
  over-length hosts, out-of-range ports, unparseable bucket times, replayed
  sequences. Buckets are clamped or dropped, never stored half-formed, and a
  replay is skipped whole. Legacy bucket bytes are summed on ingest, so a partially applied
  replay is unrecoverable; that is what the report-level idempotency guard is
  for.

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
[Docker runbook](../deploy/docker/README.md).

Performance-sensitive releases also follow [performance.md](performance.md).
Query-plan tests protect bounded access paths, while absolute P95 measurements
run against a production-shaped database on the release host.

### Installation and service migration smoke checks

Unit tests fake service managers and isolate unit/script files under `t.TempDir()`.
Before releasing installation or updater changes, validate on disposable nodes:

- systemd-networkd manages the NIC normally.
- ifupdown/networking manages the NIC while networkd wait-online is enabled:
  BoxFleet startup must not pull in or wait for that service.
- Network and a specifically configured public listen address arrive late:
  agent keeps polling and sing-box eventually starts without manual reset-failed.
- Interrupt each installer phase, including a pending systemd start job; recover
  using the saved config and confirm both services and preserved traffic state.
- Upgrade from the legacy agent: both units migrate using the new templates,
  the helper refreshes before template mutation, and upgrade succeeds only after
  a successful heartbeat and daemon-reload.
- Force a service migration reload failure and an unconfirmed candidate restart:
  old service files (including modes) and the previous binary are restored.
- Repeat installation/recovery and run competing writers; paused nodes must keep
  sing-box stopped. Repeat update/recovery on an OpenRC node.

These real-service cases belong to deployment smoke validation, not unit tests.
