# Performance SLO

BoxFleet's production admin experience has a measurable performance target.
The target workload is three times the production telemetry volume observed on
2026-07-22, while agents continue reporting and two administrators use the Web
UI concurrently.

## Release target

| Path | P95 target |
| --- | ---: |
| Client-side page navigation | 1.5 seconds |
| Cold admin UI load | 2.5 seconds |
| Ordinary admin API | 200 milliseconds |
| Overview, traffic summary, and first Network Events page | 500 milliseconds |

No database operation may cause more than two seconds of head-of-line blocking
for an otherwise independent admin read.

The reference three-times workload is approximately 1.9 million traffic
deltas, 610 thousand traffic reports, 2.6 million structured network events,
and 1.4 million heartbeats. Cardinalities such as nodes, users, proxies, and
access grants are also multiplied by three. Normal agent writes remain enabled
during the measurement.

## Measurement rules

- Measure from the browser or API client to the running server, not only the
  duration of an isolated SQL statement.
- Use at least 100 requests after a warm-up pass and report P50, P95, maximum,
  error count, and the tested row counts.
- Cold-load measurement starts with an empty browser cache. Navigation
  measurement starts after the application shell has loaded.
- Run two independent admin request streams while representative heartbeat,
  traffic, and network-event writes continue.
- A release passes only when every row in the target table passes. An average
  does not compensate for a missed P95.

## Performance invariants

The main read paths must remain bounded by current entity cardinality or by the
requested time window, rather than total telemetry history:

- Traffic summaries read `traffic_usage_totals`; ingestion updates the rollup
  in the same transaction as the raw delta.
- Node and publish status read `node_latest_heartbeats`; each heartbeat advances
  the per-node pointer transactionally.
- The first Network Events page filters by its visible time window and uses the
  partial visible-event index.
- Network Events action, node, user, and combined node/user filters use partial
  composite indexes whose time-window columns follow the selected dimension.
- Network Events free-text search uses an FTS3 document index maintained by
  database triggers; it never evaluates `LOWER(...) LIKE '%term%'` across the
  full event table.
- SQLite uses WAL and a small connection pool so one report or slow read does
  not serialize unrelated reads.
- The application shell does not wait for Overview data, route pages are split
  into separate browser chunks, and hashed assets are compressed and cached.

`go test ./internal/server/db` includes query-plan guards for these invariants.
Absolute latency still has to be measured on the release host because CPU,
storage, database size, and network conditions materially affect it.

## Deferred bottleneck: Network Events prefix search

The production FTS3 index removes the former full-table `LIKE` scan, but prefix
queries are not yet within the heavy-read SLO on the current two-core host. A
2026-07-22 diagnostic run against roughly 881 thousand visible events measured
the following for a real destination search:

- last 24 hours: P50 519 ms, P95 1.90 s, maximum 3.02 s (30 requests)
- all history: P50 518 ms, P95 2.21 s, maximum 2.53 s (20 requests)

Query decomposition showed that FTS3 `MATCH` itself took about 1.5 seconds even
when only seven rows matched. The cause is that every search token is currently
compiled as a prefix token and the virtual table has no dedicated prefix index;
the normal time-window and ordering indexes are not the limiting path.

Further index expansion is intentionally deferred. The proposed follow-up is an
FTS4 prefix index (a three-character prefix index is the first candidate), plus
a minimum useful prefix length for one- and two-character searches. Treat the
free-text Search field as a known exception to the 500 ms heavy-read target
until that work is approved and measured with the full 100-request protocol.

## Baseline that triggered this target

Before the bounded read paths were introduced, the 2026-07-22 production
database was about 2.77 GB. Overview took roughly 16.5 seconds, traffic summary
roughly 14.2 seconds, nodes roughly 3.5 to 3.9 seconds, and unbounded Network
Events and config-change reads exceeded 30 seconds. Those numbers are a
historical baseline, not acceptable operating limits.
