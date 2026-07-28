# Component compatibility and release policy

BoxFleet has three independently versioned runtime components:

- `bfs`, the control-plane server and config renderer;
- `boxfleet-agent`, the node-side reconciler and updater;
- `sing-box`, the node-side data plane.

Independent version numbers do not by themselves make releases independent.
This document separates four different promises: build publication, component
installation, wire compatibility, and generated-config compatibility.

## Current state

For the planned server `v0.11.x` release, agent `v0.7.0`, and sing-box pin
`v1.14.0-beta.2`:

| Property | Status | Boundary |
| --- | --- | --- |
| Independent version numbers | Yes | The server, agent, and upstream sing-box versions are recorded separately. |
| Server-only installation | Yes | Replacing `bfs` does not require touching a node. |
| Agent-only or sing-box-only node update | Yes | The update API accepts either component, and each is installed and rolled back independently. |
| Independent publication | **No** | One server `v*` tag builds all artifacts in one workflow and publishes one GitHub Release. |
| Independent update catalog | **No** | `boxfleet-update.json` is tied to the server release, and the server rejects a catalog whose `release` differs from its own version. |
| Agent feature negotiation | Partial | Durable operations and optional telemetry use named capabilities. The basic node API has no negotiated API range. |
| sing-box config negotiation | Partial | The normal renderer is qualified against the pinned beta.2 build. The opt-in 1.14 `services` block is not currently gated by the reported sing-box version. |

The practical answer is therefore: components can be **deployed and upgraded
separately**, but a new agent or sing-box artifact cannot yet be **published
without making a new server release**.

## Compatibility surfaces

Compatibility must be decided per interface, not by subtracting component
version numbers.

| Producer | Consumer | Contract |
| --- | --- | --- |
| agent | server | Authenticated JSON node API: config fetch, apply result, heartbeat, traffic, logs, connection reports, and operations |
| server | agent | HTTP status, response headers, rendered config bytes, and operation assignments |
| server | sing-box | Generated JSON configuration schema and semantics |
| agent | sing-box | `check`, `version`, systemd lifecycle, v2ray stats gRPC, journal format, and optional daemon connection gRPC |

The server and agent happen to use SemVer-looking product versions, but they
are different products and their minor numbers have no mathematical
relationship. sing-box is upstream software whose config removals and behavior
changes do not align with BoxFleet releases. A rule such as “difference of two
minor versions is supported” would give false confidence on all four surfaces.

## Current compatibility matrix

This table records what the repository actually builds or has shipped. It is
not a claim that every Cartesian product is continuously tested.

| Server release | Advertised agent target | Advertised sing-box target | Status |
| --- | --- | --- | --- |
| `v0.11.x` | `v0.7.0` | `v1.14.0-beta.2` | Planned release tuple; beta accepted explicitly and qualified by the five-check preflight |
| `v0.10.x` | `v0.7.0` | `v1.13.14` | Immediate server and sing-box rollback tuple |
| `v0.6.2`–`v0.9.x` | `v0.6.1` | `v1.13.13` | Historical target tuple; not a current support promise |
| `v0.5.0`–`v0.6.1` | same version as server | `v1.13.13` | Introduced capability-based managed updates; historical, not a current support promise |
| `v0.1.x`–`v0.4.x` | same version as server | `v1.13.13` | No managed-update capability; manual upgrade only |

Feature-level compatibility for the current server is narrower and more useful:

| Feature | Minimum agent contract | sing-box contract | Current status |
| --- | --- | --- | --- |
| Config pull/apply, heartbeat, traffic and journal logs | Legacy node API | Exact renderer-qualified beta.2 build | Normal fleet path |
| Managed agent update | `operations.v1`, `update.agent.v1`, `download.streaming.v1`, `install.versioned.v1`, `restart_resume.agent.v1` | Not applicable | Negotiated and enforced |
| Managed sing-box update | `operations.v1`, `update.sing_box.v1`, `download.streaming.v1`, `install.versioned.v1`, `rollback.sing_box.v1` | Candidate version, `with_v2ray_api`, and live config verified by the updater | Negotiated and enforced |
| Connection stream telemetry | `telemetry.connections.v1` | Qualified sing-box 1.14 daemon API build | Experimental, per-node opt-in, off by default |

The `v0.11.x` release does not change the advertised agent target: `v0.7.0` is
both the current and immediately previous target. The older `v0.6.1` agent is a
historical tuple, not a supported transition edge, until cross-version CI proves
otherwise.

An agent with operations support must not be installed against a pre-`v0.5.0`
server: those servers do not expose the operations endpoints, and a `404` is a
non-retryable operation error that terminates the agent daemon. This is a
concrete example of why product-version distance is not a safe compatibility
test.

For sing-box, `v1.13.14` is the immediate rollback pin and
`v1.14.0-beta.2` is the current release target. Only the exact beta.2 build is
qualified by the five-check preflight. Other patches or minors are unsupported
until the preflight moves the pin. The daemon stream remains experimental and
per-node opt-in despite the beta qualification; enabling it on 1.13 causes the
candidate config check to fail safely, but leaves the node stuck retrying that
target.

## Compatibility rules going forward

### 1. Product versions identify artifacts; contracts decide compatibility

Do not compare the server, agent, and sing-box version numbers to each other.
Every optional feature has a named capability such as `update.agent.v1`. A
breaking change creates a new capability suffix; it never changes the meaning
of an existing capability.

Until an explicit basic node-API range is implemented, the supported rolling
window is:

- current server with its advertised agent target;
- current server with the immediately previous advertised agent target, for
  upgrade and rollback only;
- no guarantee for an agent newer than the server.

The supported order is always server first, then agent. This asymmetric rule
lets the server learn a new capability before an agent advertises or uses it.

### 2. Node JSON evolves additively within a capability version

Within a `.v1` contract:

- new request and response fields are optional;
- missing fields retain their old behavior;
- readers ignore unknown fields;
- fields are not renamed, repurposed, or changed to a different type;
- enum-like strings accept unknown values or fail only the optional feature,
  not the core reconcile loop;
- removing an endpoint or making an optional field required is a new contract
  version.

The server must continue accepting reports from the previous advertised agent
target. The agent must treat unsupported optional endpoints as feature absence,
not as a reason to terminate its config/heartbeat daemon.

### 3. sing-box uses exact qualified profiles, not numeric skew

The renderer owns named config profiles, for example:

- `singbox.config.1_13` for the current normal config shape;
- `singbox.services_api.1_14` for the optional daemon connection stream.

A sing-box version is supported only when its exact version, build tags, config
profile, stats API, update/rollback path, and log fixtures pass
`docs/singbox-preflight.md`. Patch releases are not assumed compatible: they can
change log text or behavior even when JSON still parses. Major/minor releases
must never enter the production allow-list without migration review.

The server must not publish a profile unless the latest node heartbeat proves
the required agent capability and a qualified sing-box version. Until that
check exists for connection telemetry, operators must enforce it manually and
must not opt a 1.13 node into the 1.14 service.

### 4. Release support is a tested tuple plus one transition edge

Each formal release records:

```text
server version
agent target version
sing-box target version and build tags
required/optional capability set
renderer profile(s)
previous agent transition target
previous sing-box rollback target
```

CI must test the current tuple and these rolling edges:

1. previous agent -> current server;
2. current agent -> current server;
3. current server renders/checks against the current sing-box target;
4. agent update from previous to current and rollback;
5. sing-box update from previous to current and rollback;
6. optional profiles fail closed when their capabilities are absent.

Anything outside those tuples is “unverified”, not “probably compatible”.

### 5. Upgrade and rollback order

Use a canary before each fleet-wide step:

1. back up the server database and upgrade `bfs`;
2. verify old agents still fetch config and report heartbeats;
3. upgrade one agent canary, then the remaining agents;
4. upgrade one sing-box canary, validate config, counters, logs, and traffic,
   then continue in bounded batches;
5. enable features requiring new capabilities only after every target node has
   reported them.

Rollback in the reverse component order. Rolling back the server additionally
requires its matching database backup whenever a migration is not understood
by the old binary.

## Finishing independent publication

True publication decoupling requires work beyond the current independent
version variables:

1. Give server, agent, and sing-box separate tag namespaces or workflows.
2. Publish immutable component manifests keyed by component version and
   platform, rather than one manifest whose identity must equal the server
   version.
3. Let the server select an allow-listed compatibility tuple from those
   manifests. Admin requests must still never supply arbitrary URLs.
4. Record the tuple and contract/profile requirements in the release catalog.
5. Add cross-version CI before widening the supported window.
6. Keep checksum verification; add signed provenance or TUF before artifacts
   are accepted from more than the single trusted release channel.

This changes publication without weakening the existing server-selected asset,
canary, durable operation, and rollback guarantees.

## Why this resembles mature projects

Mature client/server projects combine a small, explicit skew window with
feature or API negotiation:

- [Kubernetes publishes a component-specific skew policy](https://kubernetes.io/releases/version-skew-policy/): kubelet may lag the API server within a declared window, cannot be newer, and the documented upgrade order is control plane first.
- [Nomad recommends servers before clients](https://developer.hashicorp.com/nomad/docs/upgrade/) and documents a bounded backward-compatibility window while warning that new features may require all nodes to be upgraded.
- [Docker versions the Engine API and negotiates the highest mutually supported version](https://docs.docker.com/reference/api/engine/), disabling newer features when negotiation selects an older API.
- [Protocol Buffers defines additive wire-evolution rules](https://protobuf.dev/programming-guides/proto3/#updating): old readers ignore new fields, while field identity and incompatible types are not repurposed.
- [sing-box publishes explicit migration and removal notes](https://sing-box.sagernet.org/migration/) rather than promising that every nearby version accepts the same config.

BoxFleet should follow the same shape: a short declared transition window for
operations, capability/profile negotiation for features, additive wire rules,
and exact qualification of the data-plane binary.
