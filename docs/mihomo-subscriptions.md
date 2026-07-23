# Mihomo subscriptions

BoxFleet treats a complete Mihomo configuration as the primary subscription
format. Each configuration binds one proxy user, while one user may own any
number of configurations and independent links. The server renders the user's
active VLESS-Reality accesses as an inline top-level `proxies` array, then runs
the configuration's enabled YAML and JavaScript processors in order.

There is no invisible built-in processor on this path. New configurations start
with a link to the global `BoxFleet Basic` template, so the fast path is ready
to save while the complete pipeline remains visible and switchable.

The legacy proxy-provider response remains available at `/sub/{token}`. The
complete profile is available at `/sub/{token}/mihomo.yaml` and is returned as
`mihomo_url` by the admin subscription API.

## Rewrite contracts

YAML uses the Clash Party/Sub-Store merge contract:

- Scalars replace existing values.
- Objects merge recursively.
- Arrays replace by default.
- `+key` prepends an array and `key+` appends an array.
- `key!` replaces an object instead of merging it.
- `<key>` removes ambiguity when the literal key itself starts or ends with a
  modifier character.

JavaScript is synchronous and must define `main(config)` and return a plain
configuration object. The runtime exposes standard ECMAScript built-ins and a
bounded `console`; it does not expose Node.js `require`, network, filesystem,
process, database, environment, or timer APIs. Source, input, output, log, and
execution-time limits are enforced.

## Sub-Store compatibility evidence

The checked reference version is Sub-Store commit
`7db77f5cbd60d40d56bbf95c74eb958f9b70cd39`. Its source can be cloned for
read-only reference and black-box testing:

```bash
git clone https://github.com/sub-store-org/Sub-Store.git refs/sub-store
git -C refs/sub-store checkout 7db77f5cbd60d40d56bbf95c74eb958f9b70cd39
pnpm --dir refs/sub-store/backend install --frozen-lockfile
BOXFLEET_SUBSTORE_COMPAT=1 go test ./internal/server/mihomo \
  -run TestLiveSubStoreCompatibility -count=1 -v
```

`refs/` is ignored and Sub-Store is never imported or bundled into BoxFleet.
The runner in `scripts/compat/substore-mihomo-runner.cjs` invokes the external
checkout and exchanges only JSON/YAML test inputs and outputs.

| Case | Result | Notes | Follow-up |
| --- | --- | --- | --- |
| Recursive object merge; scalar and array replacement | Pass | Normalized BoxFleet and Sub-Store YAML are equal. | Keep in the live regression set. |
| `+array`, `array+`, and `object!` modifiers | Pass | Normalized outputs are equal. | Keep in the live regression set. |
| Synchronous `main(config)` mutation | Pass | Normalized outputs are equal. | Add fixtures when BoxFleet adopts more pure script helpers. |
| `async function main(config)` | Intentional difference | Sub-Store awaits it; BoxFleet returns `async_unsupported` to keep public subscription compilation bounded and deterministic. | Not planned unless scripts move to a separately resource-limited worker process. |
| JavaScript without `main(config)` | Intentional difference | Sub-Store leaves the input unchanged; BoxFleet returns `invalid_script` so a saved rewrite cannot silently do nothing. | No change planned; explicit failure is the save contract. |
| Infinite loop | BoxFleet-only safety pass | BoxFleet interrupts at the configured deadline. The case is not run against Sub-Store because its in-process dynamic function would hang the comparison process. | Retain as a mandatory safety test. |

The recorded fixtures live in
`internal/server/mihomo/testdata/substore_compatibility.json`. A compatibility
claim is not added to this table until both the recorded test and live external
comparison have been run.

Sub-Store application globals and host capabilities such as `$`, `ProxyUtils`,
remote resource fetching, filesystem/process access, timers, and Node module
loading are explicitly unsupported. They are not part of Clash Party's YAML or
`main(config)` data contract and would turn a public subscription fetch into an
administrator-controlled I/O runner. A future helper is considered only if it
is pure, allowlisted, bounded, and represented by both a BoxFleet test and a
live comparison fixture.

## Save and live-template model

A Mihomo configuration contains one saved document. Saving validates it against
the configuration's bound user's current proxies and immediately makes it
eligible for a subscription link. There is no Mihomo draft, publish, revision,
or rollback operation. Subscription tokens belong to configurations, not users,
so two configurations based on the same user's proxies can expose different
pipelines and URLs.

Saving requires a successful render and rejects YAML/JavaScript execution errors
or structural diagnostics. Delivered profiles are checked again so invalid data
written outside the admin API fails closed instead of silently skipping a
rewrite.

Global rewrite templates are reusable live definitions. Inserting one stores its
`template_id` in the configuration. Template processors are read-only inside the
configuration; custom processors are scoped to that configuration and editable
there. Preview and subscription rendering resolve every linked processor from
the latest saved template, so one template edit consistently updates all linked
configurations without changing their order or enabled state.

## Admin workbench and cache

`/admin/mihomo-profiles` is route-level lazy loaded so Monaco does not affect
the normal overview, node, proxy, or user pages. Its default horizontal tab is a
configuration table; the second is the global rewrite-template table. Creating
and editing use the independent `/admin/mihomo-profiles/new` and
`/admin/mihomo-profiles/:profile/edit` routes while retaining the standard admin
shell, page header, width, spacing, and Kumo components. Creating a configuration
first chooses the user whose `proxies` form the base, then builds the initial
ordered pipeline. The editor uses a left processor list and a right Monaco
editor. Every processor has an enable switch and ordering controls; linked
templates are read-only and custom processors are editable. `Preview config`
compiles the current editor state and shows the complete final YAML plus
diagnostics before saving.

The bundled Mihomo schema is supplied by the MIT-licensed `meta-json-schema`
package and is augmented with Clash Party modifier keys.

Successful compilations use a bounded in-memory LRU. Its SHA-256 key covers the
compiler semantic version, the rendered inline-proxy base, and every ordered
rewrite field. Proxy changes, saved configuration changes, template changes, or
compiler changes therefore miss the cache naturally. Compilation failures and
canceled requests are never cached, and returned byte/log/diagnostic slices are
copied to prevent mutation of cached state.
