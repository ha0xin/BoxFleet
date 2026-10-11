# Component compatibility

The server, agent and sing-box are released independently. Do not infer a node
component target from the server's version number.

## Sources of truth

- `.github/workflows/artifacts.yml` selects the agent and sing-box revisions and
  sing-box build tags for release artifacts.
- A release's `boxfleet-update.json` records its component versions and artifact
  checksums. Use that manifest for an exact release combination.
- Heartbeats report installed versions and agent capabilities. Version reports
  describe the node; they do not automatically enable optional features.
- `internal/singboxapi/README.md` records the vendored daemon protocol provenance.

Keep deployment inventory, rollout progress and rollback artifact locations in
operator records rather than in general project documentation.

## Contracts

Server-to-agent contracts include configuration pull/apply, heartbeats, traffic,
logs and durable operations. Shared payloads live in `internal/model`. Preserve
additive compatibility when changing them, and test missing optional fields
against legacy payload fixtures.

Managed updates depend on the capabilities advertised by the installed agent.
Agents without the required capability need a manual upgrade. Keep the exact
capability names used on the wire; these are protocol identifiers, not product
release numbers.

Connection streaming requires both a collector-capable agent and a sing-box
build that supports the daemon API. The agent capability
`telemetry.connections.v1` describes only the collector. The renderer enables
`services` only for an enabled `node_connection_telemetry` row; it does not
negotiate that block from reported versions. Operators must verify support
before enabling it. Default configs omit the optional service.

## Upgrade and rollback

Deploy the server and its migrations before agents that send new payloads.
Validate a canary before releasing further node batches. A successful heartbeat
alone does not prove configuration, counters or connection collection work.

Qualify candidates with rendered configuration checks, real protocol traffic,
V2Ray counter reads, connection-stream checks when enabled, service restart and
rollback checks on each supported service manager. Unit tests use fake command
runners and daemon fixtures; they do not prove a real binary supports the contract.
See [testing](testing.md) and [deployment](deployment.md).

Retain the previous verified component artifacts. Configuration or executable
rollback does not downgrade a migrated database: follow migration-specific
rollback rules and restore a matching pre-migration backup when necessary.
Disabling connection streaming also requires publishing a config without its
service before installing a build that cannot parse that service.

## Independent publication

Component targets are already independent of the server version. Further
separation of release pipelines is a design option, not a claim that separate
component release workflows or automatic configuration negotiation exist.
