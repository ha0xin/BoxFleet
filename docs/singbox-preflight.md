# sing-box preflight

`SING_BOX_REVISION` in `.github/workflows/artifacts.yml` pins the one sing-box
build BoxFleet ships. `scripts/singbox-preflight.sh` is the gate that pin passes
before it moves — to a 1.13 patch, and eventually to 1.14. It implements the four
off-fleet checks required by
[ADR 0001](adr/0001-network-event-telemetry-source.md#off-fleet-checks-required-before-the-pin-moves-past-113).

The harness exists because two of the four failure modes are silent. The agent's
health check asserts systemd `ActiveState` only, so a sing-box that starts
cleanly but renamed a build tag or reworded a log line reports healthy on every
node while traffic accounting or the entire network-event and service-audit view
reads zero. Nothing else in the update pipeline catches either one.

**Throwaway host only.** The script never contacts a BoxFleet server and never
touches a fleet node. It builds into a temporary directory, runs the candidate
under a dedicated transient systemd unit, and drives traffic through loopback
with credentials it generates and discards. Do not run it on the management
server, do not point it at a node, and do not reuse the configs it writes.

## Prerequisites

| Checks | Needs |
| --- | --- |
| 1 | Go toolchain, `git`, network access to `github.com/SagerNet/sing-box` |
| 2 | Go toolchain with CGO enabled (the SQLite driver is `mattn/go-sqlite3`) |
| 3, 4 | everything above, plus a Linux host with systemd and `journalctl`, `curl`, root, outbound internet, and a checkout of this repository |

Checks 1 and 2 run unattended on any developer machine, macOS included. Checks 3
and 4 need a live instance and real traffic, so they run only under `--live` on a
Linux host.

## Running it

```bash
# Checks 1 and 2 against the currently pinned revision.
scripts/singbox-preflight.sh

# Checks 1 and 2 against a candidate.
scripts/singbox-preflight.sh v1.14.0

# All four, on a throwaway Linux host.
sudo scripts/singbox-preflight.sh --live v1.14.0

# Reuse an already-built candidate instead of cloning and building again.
sudo scripts/singbox-preflight.sh --live --binary /tmp/sing-box-candidate v1.14.0
```

The revision argument defaults to `SING_BOX_REVISION` parsed out of the workflow,
and the build tags default to `SING_BOX_TAGS` from the same file. Neither is
spelled twice; editing the workflow is enough.

Exit codes:

- `0` — all four checks passed. Moving the pin is an ordinary release change.
- `1` — a check failed, or `--live` was requested and could not be completed.
- `2` — the preflight is incomplete: checks 3 and 4 were not run. A check that
  did not run is not a check that passed, so this is never a green result.

Artifacts (the built binary, rendered configs, journal capture, counter
snapshots, logs) stay in the work directory whenever anything failed, and with
`--keep` always.

## Check 1 — build tags intact

**Protects:** every feature BoxFleet reaches through a build tag.
`with_v2ray_api` backs per-user traffic counters and therefore billing;
`with_clash_api` backs connection tracking and is the prerequisite for the 1.14
telemetry migration.

**How it runs:** clones sing-box, checks out the candidate, and builds
`./cmd/sing-box` with the workflow's exact `SING_BOX_TAGS` and `release/LDFLAGS`,
then asserts the reported version and every required tag appear in `sing-box
version` output.

The required-tag list is *read out of the workflow itself* — the harness greps
`artifacts.yml` for its `grep -F 'with_…'` assertions instead of restating them.
Adding a tag BoxFleet depends on to CI's assertion block extends this check with
no edit here, and the two lists cannot drift. The script also fails if CI asserts
a tag that is not in the tags it builds with.

**A failure means:** `go build -tags` silently ignores unknown tags, so an
upstream rename compiles clean and drops the feature. Find the new tag name
upstream and update `SING_BOX_TAGS` and CI's assertions together. Never work
around it by removing the assertion.

## Check 2 — config compatibility

**Protects:** the renderer's output surviving an upstream option change.

**How it runs:** `scripts/singbox-preflight render-configs` seeds a throwaway
SQLite database with the same fixtures `internal/server/render` tests use — a
VLESS-Reality node with two users, a Shadowsocks 2022 node, and one client config
per protocol — and writes real `internal/server/render` output, including
`RenderDisabledConfig`. Reality keys are generated exactly as the admin API
generates them (`internal/secret.RealityKeyPairX25519`), so the candidate sees a
well-formed private key rather than the placeholder strings the Go tests use.
Every file is then run through `sing-box check -c`.

**A failure means:** the candidate rejects a config BoxFleet renders today.
Because nodes run `sing-box check` before applying, this surfaces as a node that
refuses the new config rather than as data loss — but it stalls the whole fleet
at rollout. Fix `internal/server/render` first, with a renderer test, then re-run.

## Check 3 — log format unchanged

The highest-value check and the one no gate catches.

**Protects:** `log_events` — every network event, every source IP, every
`target_host`, and the whole domain/service audit view. These exist only because
`parseSingBoxLogEvent` (`internal/server/db/log_events.go`) scrapes them out of
unstructured journal text.

**How it runs, in two phases:**

1. Unattended: `go test ./internal/server/db -run
   TestParseSingBoxLogEventGoldenFixtures` replays the committed fixtures. If the
   parser already disagrees with its own goldens, that is BoxFleet's regression
   and nothing about a candidate can be concluded until it is fixed.
2. Under `--live`: the candidate runs as a transient systemd unit, a second
   instance runs the rendered client config, real requests are driven through the
   proxy, and the unit's journal is captured with `journalctl -o json`. The
   capture is converted into the `testdata/singbox_logs/*.input.txt` fixture form
   — one raw `MESSAGE` per line, ESC spelled as the literal characters `\x1b` —
   and replayed through the real parser.

The parser is unexported, so the only way to run a capture through the real thing
is as a fixture in its own package. The script therefore copies the capture into
`internal/server/db/testdata/singbox_logs/` as `preflight-candidate.input.txt`,
generates only that file's golden, and removes both again from an `EXIT` trap. It
checksums every committed golden either side of that call and fails loudly if any
changed. Both files land in the work directory as evidence:

- `preflight-candidate.input.txt` — the raw capture, in fixture form.
- `preflight-candidate.golden.txt` — what the parser made of it.
- `preflight-candidate.shape.txt` — the golden with values stripped and lines
  counted, keeping `action` intact. Two captures from two different builds are
  never byte-identical, but their shapes are comparable.

Capture the shape from the currently pinned build once, keep it, and pass it to a
candidate run as `--baseline-shape FILE` to diff the two builds directly.

**A failure means:** the log wording changed. Zero parsed connect events with
real traffic flowing is the silent catastrophe this whole file exists for: the
candidate would run green on every node while network events and the service
audit drop to zero. Zero tracked sources means the correlation regex broke and
every event would store an empty `source_ip`.

Fix the regexes in `internal/server/db/log_events.go` and add a fixture for the
new wording. **A golden diff is a real regression to investigate, never something
to regenerate away.** The harness deliberately refuses to be the thing that
rewrites a committed golden.

Passing check 3 proves the parser still matches *some* lines, not that no line
kind was lost. Read the capture.

## Check 4 — traffic counters intact

**Protects:** per-user traffic accounting and quota, which read
`user>>>NAME>>>traffic>>>uplink` and `…>>>downlink` through
`internal/v2raystats`.

**How it runs, under `--live` only:** counters are snapshotted through the same
client and the same non-resetting `QueryStats` call the agent makes (`reset=true`
is never used — a lost response would lose traffic), real traffic is driven, and
the snapshot is repeated. Both the uplink and the downlink counter for the
exercised user must exist afterwards and must be strictly greater than before.

sing-box creates counters lazily on the first routed connection, so an absent
counter *before* traffic is normal and an absent counter *after* traffic is the
failure. Only users that actually carried traffic appear.

**A failure means:** counter naming moved upstream, or accounting stopped. Either
way every node reports zero bytes with no error, and per-user billing silently
stops. Check the counter names in `experimental/v2rayapi/stats.go` upstream
before changing anything in `internal/agent` or `internal/v2raystats`.

## The Go helper

`scripts/singbox-preflight/` is a small `package main` the script drives, so the
harness exercises the real renderer, the real stats client and the real journald
message shape instead of a shell approximation:

```bash
go run ./scripts/singbox-preflight render-configs -out DIR [-host HOST] [-mixed-port PORT]
go run ./scripts/singbox-preflight query-stats [-addr HOST:PORT] [-pattern user>>>]
go run ./scripts/singbox-preflight journal-fixture [-in FILE]
```

`journal-fixture` decodes both journald `MESSAGE` encodings — the JSON string and
the array-of-bytes form journald falls back to for output it does not consider
printable UTF-8, which is what sing-box's ANSI-colored lines can produce.

## Relationship to the 1.14 migration

ADR 0001 keeps network events on the journal scraper and traffic on the V2Ray
counters, and states one trigger for revisiting: the `v1.14.0` **stable** tag is
published *and* a build of it at BoxFleet's `SING_BOX_TAGS` passes these four
checks. Both conditions. "The beta looks quiet" is not one of them.

This harness is the second half of that trigger. It is equally the gate for an
ordinary 1.13 patch bump — a patch release can reword a log line just as easily
as a minor one can.

Passing the preflight authorizes moving the pin. It does not authorize adopting
1.14's `service.api` connection stream: that needs a renderer `services` block,
loopback binding, a mandatory per-node secret, a node-side aggregator and an
ingest path, none of which this script covers.
