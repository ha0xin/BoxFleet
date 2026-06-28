# Agent Handoff

Last updated after wiring real CRUD mutations (Proxies/Nodes/Users + user proxy
access), the config-publish closure bar, and the node pause/decommission +
pending-enrollment lifecycle. Built on top of commit
`da0445e Rework admin operations UI`.

This document is for the next coding agent taking over the BoxFleet admin UI
work. Read it together with `AGENTS.md`, `docs/web-ui.md`, and
`docs/design-system.md`.

## Fast Start

Use these commands first:

```bash
git status --short
npm --prefix web run build
go test ./...
go vet ./...
```

For visual work, start Vite and inspect with Playwright + system Chrome:

```bash
npm --prefix web run dev
# open http://127.0.0.1:5173/admin/
```

In Playwright, launch Chrome with:

```ts
chromium.launch({ executablePath: "/usr/bin/google-chrome-stable" })
```

Kumo is the source of truth for component APIs and examples. From `web/`, run:

```bash
npx kumo ai
npx kumo docs <Component>
```

Do this before changing unfamiliar Kumo components. Do not infer compound
component names or variants from memory.

## Current State

The admin UI has been moved to native Cloudflare Kumo components. The old
shadcn-shaped compatibility layer is no longer the intended surface for app UI.

Current first-class routes:

| Route | State |
|-------|-------|
| `/admin/` | Implemented overview/dashboard with Kumo `LayerCard`, shared helpers, and Recharts sparklines. |
| `/admin/nodes` | Implemented paginated inventory table with filters and Kumo actions. |
| `/admin/proxies` | Implemented paginated proxy table with filters and action menu column. |
| `/admin/users` | Implemented user table with traffic enrichment and a two-colour Kumo `Meter` for upload/download. |
| `/admin/traffic` | Still placeholder. This is the main missing page. |
| `/admin/network-events` | Implemented reference page for URL-synced filters, server pagination, TanStack Table, Kumo collapsible filters, date range picker, and Recharts activity chart. |
| `/admin/system-logs` | Implemented logs table with collapsible filters and Kumo controls. |
| `/admin/settings` | Implemented existing settings/token controls on Kumo shell. |

## Config Publish Closure (global bar)

Every operation that changes node config now funnels through one closed loop on
the 58px top bar (the one aligned with `Sidebar.Header`):
**idle → dirty (blue) → publishing → applying → applied (green + slide-to-unlock
sheen) / failed (red)**.

Design choice: the status is **global** but rendered **in place**. A
`PublishStatusProvider` (in `App.tsx`, wrapping `<main>`) holds the state
machine + polling + publish mutation; the existing top bars stay where they are
and just read context to tint and to slot in the Apply control. No bar was
"hoisted" and no alignment/sidebar geometry was touched.

Key files (all new under `web/src/publish/`):

- `publish-status.tsx` — `PublishStatusProvider` + `usePublishStatus()`. Polls
  `GET /api/admin/config/changes` (15s). On publish, POSTs
  `/api/admin/config/publish`, then polls `GET /api/admin/nodes` (4s) until every
  tracked node reports `apply_status === "applied"` and
  `current_version === target_version`; a node reporting `failed` flips to red.
- `publish-strip.tsx` — `PublishStrip` (right-aligned bar content per status) and
  `publishBarToneClass(status)` (bar background tint). Renders nothing when idle.
- `publish-diff-dialog.tsx` — Kumo `Dialog`, row-level diff via the `diff`
  package (`diffLines`), rendered once globally; reads `change.target_config` vs
  `change.rendered_config` straight from `/config/changes`.

The sheen animation is pure CSS (`.publish-bar-unlock` in `globals.css`), no
animation library. `transition-colors duration-300` on the bar smooths the tint.

**Why mutations don't need to know they're "dirty":** the renderer's access
query (`ListProxyAccessesByNodeName`) filters on
`enabled / proxy_user_status='active' / node_status='active' / proxy_enabled /
binding enabled`. So disabling a user/node/proxy, or revoking an access, removes
that access from the rendered config and the diff appears automatically. Any
mutation just needs to `refetch(["admin","config-changes"])` (and invalidate
`["admin"]`) on success — the bar lights up only if the change actually altered
what the server would render. `display_name` / `quota` edits do not touch render
and correctly do **not** light the bar.

### Wiring real mutations (done)

All three resources now have working create/edit/delete dialogs, and Users also
manages proxy access (issue/revoke). The shared mutation hook lives at
`web/src/admin/use-admin-mutation.ts`; on success it invalidates `["admin"]` and
the closure bar reacts automatically. Dialog components:
`web/src/pages/{proxy,node,user}-dialogs.tsx`.

Notable decisions that landed:

- `PATCH /api/admin/users/{user}` dispatches status/quota/expire/display_name to
  the db facade in one transaction (`db.UpdateProxyUser`); `display_name` has a
  setter (`SetProxyUserDisplayName`).
- Node `PATCH` uses pointer presence semantics (omitted = preserve, explicit
  `""` = clear), so a status-only toggle never wipes `api_base_url`.
- The node "Decommission" action (DELETE) revokes tokens; the menu uses the new
  `has_active_token` field to avoid offering Enable for a decommissioned node.
- Proxy edits merge the SNI into the existing `settings_json` so the server keeps
  the existing Reality key pair instead of regenerating it.

Backend route reality (for reference):

- Proxies are node sub-resources: `POST /nodes/{node}/proxies`,
  `PATCH /nodes/{node}/proxies/{proxy}`, `DELETE /nodes/{node}/proxies/{proxy}`.
- Nodes: `POST /nodes`, `POST /nodes/bootstrap`, `PATCH /nodes/{node}`,
  `DELETE /nodes/{node}`.
- Users: `POST /users`, `PATCH /users/{user}`, `DELETE /users/{user}`.
- Access: `POST /users/{user}/proxies` (issue), `DELETE /users/{user}/proxies/{node}/{proxy}` (revoke).

Request payload structs live near `admin.go:148-230` (`adminNodePatchPayload`,
`adminProxyPayload`, `adminUserPatchPayload`, `adminIssueAccessPayload`); mirror
them in `types.ts`/forms. Any new mutation should go through `useAdminMutation`
so it invalidates `["admin"]` and the closure bar reacts uniformly. Keep
`web/mocks/admin.ts` in sync (its publish handler advances node fixtures so the
green path is demoable in dev).

Important shared files:

- `web/src/App.tsx` - app shell, Kumo sidebar, route registration.
- `web/src/navigation.ts` - sidebar page list and groups.
- `web/src/pages/operations-common.tsx` - shared operation-page helpers:
  status derivation, top bars, Kumo headers, sparklines, formatting helpers.
- `web/src/pages/network-events.tsx` - best current reference implementation
  for complex pages.
- `web/mocks/admin.ts` - Vite dev mock API. Keep it updated when API shapes
  change; `npm run build` type-checks it.
- `internal/server/api/router.go` and `internal/server/api/admin.go` - admin API
  surface used by the UI.

## Design And UI Rules

The user has been reviewing visual details closely. Preserve these decisions:

- Use native Kumo components from `@cloudflare/kumo` first.
- Use Kumo semantic tokens only: `bg-kumo-base`, `text-kumo-default`,
  `kumo-hairline`, `text-kumo-success`, etc.
- Do not use raw Tailwind colours, hardcoded hex colours, or `dark:` variants.
- Use `@phosphor-icons/react` for app icons. Avoid adding new `lucide-react`
  usage.
- Do not over-style Kumo components with arbitrary padding/margins unless
  measurements show it is necessary.
- For `LayerCard`, prefer the structured form:
  `LayerCard`, `LayerCard.Secondary`, `LayerCard.Primary`.
- For tables with potentially many rows, use pagination instead of overview
  cards or long list cards.
- Filters that can grow tall should live behind a Kumo `Collapsible`, matching
  the Network Events/System Logs direction.
- Status in tables should be shown with Phosphor status icons
  (`CheckCircleIcon`, `WarningCircleIcon`, `XCircleIcon`) plus text, not chunky
  pills.

Sidebar details:

- The sidebar brand uses Kumo's `data-state` driven CSS transitions instead of
  conditional React layout. This prevents the logo from jumping before the
  sidebar width animation.
- In the expanded state, the BoxFleet logo is aligned to the same icon track as
  `Sidebar.MenuButton` icons. Do not replace this with a new hand-rolled aside.

## Backend/Data Notes

Recent backend/UI work removed the user-level traffic multiplier. The effective
traffic multiplier is now:

```text
proxy_accesses.traffic_multiplier
?? user_node_bindings.traffic_multiplier
?? proxies.traffic_multiplier
?? 1.0
```

The migration for that change is:

```text
migrations/012_remove_proxy_user_traffic_multiplier.sql
```

After changing `queries/*.sql` or migrations, regenerate sqlc:

```bash
$(go env GOPATH)/bin/sqlc generate
```

Structured network events are retained according to
`network_event_retention_days` in settings. Raw network logs are only an
intermediate ingestion source and should not be treated as durable UI data.

## Verification Already Used

The current state has been verified with:

```bash
npm --prefix web run build
git diff --check
go test ./...
go vet ./...
```

`npm run build` currently emits a Rollup large chunk warning. That is expected
after keeping Recharts for the dashboard/activity charts. If bundle size becomes
a priority, prefer route-level code splitting or vendor chunking over removing
the charting library and hand-rolling chart behaviour again.

## Next Work

CRUD mutations, the publish closure bar, and the node lifecycle (pause /
decommission / pending → active on first heartbeat) are done. The main remaining
UI work is the Traffic page.

1. Build the Traffic page (the one first-class route still a placeholder).
   - Keep `/admin/traffic` as a first-class sidebar route.
   - Show detailed traffic statistics rather than duplicating the Users page.
   - Likely sections: total billable/raw traffic, upload vs download split,
     top users, top nodes, top proxies/accesses, and time-window controls.
   - Reuse Recharts for charts; the project intentionally kept it for future
     richer traffic visualisation.
   - Use Kumo `Table`, `Meter`, `Select`/`Combobox`, `Collapsible`, and
     `Pagination` as needed.

2. Decide whether Traffic needs new backend endpoints.
   - Existing endpoints include `/api/admin/traffic/users` and per-user traffic.
   - A real Traffic page may need server-side grouping by node, proxy, access,
     direction, and time bucket.
   - Add sqlc queries and API response types instead of aggregating large raw
     result sets in the browser.

3. Deferred fixes (acknowledged, intentionally postponed):
   - **Re-enroll / re-show install command — DONE.** `POST /api/admin/nodes/{node}/reenroll`
     (`adminReenrollNodeHandler`) re-issues a bootstrap string for an existing
     node. Allowed only for a `pending` node (its one-time bootstrap was lost
     before first check-in) or a decommissioned node (`disabled` + no active
     token); an `active` node is rejected (422) and a paused node should be
     re-enabled instead. Because the raw bootstrap token is never stored, it
     rotates the token (revoke existing + issue fresh — net one token, never
     accumulating) and returns a decommissioned node to `pending`. UI:
     `ReenrollNodeDialog` + node-menu items "Show install command" (pending) /
     "Re-enroll" (decommissioned). Bootstrap stays strict (still 422s on a name
     collision) so it never clobbers a live node. The enroll/re-enroll dialogs
     now show one copy-paste **Install command** (`curl … -o … && sudo sh …
     'bootstrap'`) instead of separate string/URL fields.
   - **Multiple hosts per node — DONE.** A node can publish several addresses
     (domain, IPv4, IPv6) via the `hosts_json` column (migration 013); each host
     marked "selected" gets its own client profile in `render.NodeInfoForUser`
     (profile names are suffixed `@ <host>` when more than one). `public_host`
     mirrors the first host so views/search/sort are untouched; `db.NodeHost` +
     `normalizeNodeHosts` own validation (≥1 host, ≥1 selected, dedup). Enroll
     stays single-host (primary); multi-host is managed in the node **Edit**
     dialog (a `useFieldArray` host list with a per-row "Profile" Switch). API:
     `adminNode.hosts` + `adminNodePatchPayload.hosts`.
   - **Publish polling re-renders every node every 15s.**
     `PublishStatusProvider` polls `/api/admin/config/changes`, which runs a full
     `RenderNodeConfig` for every non-disabled node unconditionally. Fine for
     small N; for scale, drive it from a server-side dirty signal instead of
     unconditional timer-driven full renders.
   - **Extra `GetNode` per agent config poll.** `nodeConfigHandler` does a
     `GetNode` just to check disabled status before `GetTargetConfig`; could read
     status from a row an existing query already loads.

4. Open design questions (need product sign-off — see
   `docs/code-review-findings.md`):
   - Publish gate: should a re-enabled node refuse to apply a stale published
     target until republished, and should target-less nodes get a waiting config
     instead of the live render? Both are current-by-design behaviours.
   - CLI slimming: `bf` is to be trimmed to ops/diagnostics once confirmed; Web
     UI owns day-to-day CRUD. Docs still describe the full CLI for now.

5. Add focused tests for new API/query behaviour.
   - For backend traffic aggregation, test in `internal/server/db` and
     `internal/server/api`.
   - For frontend-only changes, `npm --prefix web run build` is the minimum;
     use Playwright screenshots/geometry checks for layout-sensitive Kumo work.

## Planned: Agent Self-Update (UI-triggered)

Agreed design, not yet built. Today agents never self-update: `install.sh`
injects the running server version so *new* nodes download a matching
`boxfleet-agent` from the GitHub Release, but an already-bootstrapped agent has
no self-update path (`internal/agent/agent.go` only knows its service name).

Target UX: after the server is upgraded, the Web UI shows the new version and an
**Update** button per node plus an **Update all** action; clicking it makes that
node's agent upgrade itself on its next poll.

Confirmed decisions:

- **Target version is pinned to the server version** (the release bundles a
  matching agent). A node is "outdated" when its reported `agent_version`
  differs from the server version. No free-form version entry.
- Phase the work: **Phase 1** (low risk) exposes the server version to the UI
  and shows "update available" per node by comparing to `agent_version` — no
  action yet. **Phase 2** adds the trigger + agent self-update + buttons.
- **No automatic rollback in v1** — SHA256-verify the download, sanity-check the
  new binary (`<new> version`) before swapping, keep a backup of the old binary,
  and document manual recovery. A `systemd OnFailure`-driven auto-rollback is a
  later v2 consideration.

Proposed mechanism (pull-based, matching the agent-only-pulls architecture):

1. Admin clicks Update → server marks the node "agent-update requested" (a new
   nodes column / flag). "Update all" flags every outdated active node.
2. On the next `GET /api/node/config` poll, if the flag is set and the reported
   version differs, the response carries a directive header
   (`X-BoxFleet-Agent-Update: <server-version>`).
3. The agent downloads `boxfleet-agent-<version>-linux-amd64` from the Release
   (same source as `install.sh`), SHA-verifies it, backs up + atomically
   replaces its own binary, and `systemctl restart boxfleet-agent`.
4. After restart the agent's heartbeat reports the new version; the server
   clears the flag once `agent_version == server-version` (version match is the
   ack, so it cannot loop).

Risks to keep in mind when building Phase 2:

- A broken new binary leaves that node's agent down with no remote fix (the
  agent is the thing that updates) — hence SHA-verify + sanity-check + backup,
  and **test on one node first**.
- Sequence the self-update so it does not interrupt an in-flight config apply.

## Known Caveats

- `refs/` contains reference source only. Do not import from it.
- `cf-*.html` files may exist locally as Cloudflare page captures for visual
  reference. Treat them as source material for layout inspection, not as code to
  paste wholesale.
- The Vite dev mock returns generic `{ ok: true }` for write requests. A UI flow
  can appear to succeed in dev while still needing real backend validation.
- The sidebar and LayerCard work was tuned with screenshots and DOM geometry.
  If changing those areas, re-measure with Playwright instead of eyeballing.
