# sing-box Build

BoxFleet pins sing-box in `.github/workflows/artifacts.yml` and publishes a
Linux amd64 binary with the node release. `SING_BOX_REVISION` changes only after
the new upstream version passes config, traffic, update, and rollback tests.

Two build tags are required and CI verifies both:

- `with_v2ray_api` — without it the agent cannot read per-user traffic counters.
- `with_clash_api` — connection tracking lives in the Clash API server, so this
  tag is the prerequisite for any future connection-telemetry migration. It is
  compiled in but **inert**: rendered configs carry only an `experimental.v2ray_api`
  block, never `experimental.clash_api`, so no Clash listener runs on a node.
  Do not enable it in the renderer without reading
  [ADR 0001](../../docs/adr/0001-network-event-telemetry-source.md) — it costs a
  stop-the-world `runtime.ReadMemStats()` per request and up to 1000 retained
  connection metadata copies, and it exposes a control plane, not read-only
  telemetry.

The workflow also enables the networking features listed in `SING_BOX_TAGS`.
`with_naive_outbound` is intentionally omitted because the supported
VLESS-Reality path does not need it and it introduces additional linker
requirements.

Verify a candidate with:

```bash
sing-box version | grep with_v2ray_api
sing-box version | grep with_clash_api
sing-box check -c <generated-config.json>
```

Network events are scraped from sing-box's journal text, so a revision bump can
change the log format. Run `go test ./internal/server/db -run
TestParseSingBoxLogEventGoldenFixtures` against the candidate's log output before
promoting it; a golden diff is a real regression signal.

Generated configs expose V2Ray stats only on `127.0.0.1:18082` and enumerate
every rendered access `auth_name`. Counter naming is:

```text
user>>>AUTH_NAME>>>traffic>>>uplink
user>>>AUTH_NAME>>>traffic>>>downlink
```

The agent maps these counters back to access rows. Config details belong in
[configuration rendering](../../docs/config-generation.md); release and install
steps belong in [deployment](../../docs/deployment.md).
