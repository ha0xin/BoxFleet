# Web UI

The React/Vite admin UI is served under `/admin` (or the configured hidden
prefix) and embedded in `bfs`.

```bash
npm ci --prefix web
npm --prefix web run dev       # mock API
npm --prefix web run dev:api   # proxy to server on :18081
npm --prefix web run build
```

Build output goes to ignored
`internal/server/webui/assets/generated`. Build it before compiling or testing
the server.

## Stack and boundaries

- Cloudflare Kumo for UI components; Base UI primitives only when Kumo lacks a
  styled component.
- Tailwind v4 with Kumo semantic tokens only.
- TanStack Query for API state and invalidation.
- TanStack Table for non-trivial tables.
- react-hook-form and zod for forms.
- date-fns and react-day-picker for local-time filters.
- Phosphor icons for application icons.
- Monaco/monaco-yaml for the lazy-loaded Mihomo editor.

Admin requests live behind `AdminApiProvider`. Endpoints and query keys belong
in `src/admin`; mutations use `useAdminMutation` so cache invalidation completes
before dialogs close. API failures preserve HTTP status, and a 401 routes the
operator to Settings.

## Visual rules

Use native Kumo components and semantic tokens such as `bg-kumo-base`,
`text-kumo-default`, and `kumo-line`. Never use raw Tailwind colours, hardcoded
component colours, `dark:` variants, native `<select>`, or a parallel component
wrapper library.

Use Kumo's component documentation instead of guessing APIs:

```bash
cd web
npx kumo ai
npx kumo docs <Component>
npx kumo ls
```

Navigation is defined in `src/navigation.ts`; routes are registered in
`src/App.tsx`. Pages use `AppPageHeader` and the shared 1400px content width.
The 58px breadcrumb bar aligns with `Sidebar.Header`. Mobile navigation must
remain reachable, tables own their horizontal scrolling, and dialogs use a
viewport-bounded scroll area.

Tables should reuse `components/admin-table.tsx`. Keep identity columns sticky
when row actions require horizontal scrolling. Empty tables show `0 items` and
still allow page-size selection.

## Page contracts

- Nodes owns enrollment, pause/resume, decommission/re-enroll, config and managed
  component updates.
- Proxies owns listener/protocol inventory; transport is server-derived and
  read-only.
- Users owns identity, quota, access grants, connection details and legacy user
  subscriptions.
- Mihomo Profiles owns complete configuration pipelines, live templates,
  preview, and configuration-scoped subscriptions.
- Network Events is the reference server-paginated, URL-synchronised table.
- Traffic remains a placeholder until bounded aggregation APIs exist.

The global publish bar compares current rendered configs with published targets.
Writes need not predict whether they are dirty; root invalidation lets the
server-derived diff decide.

## Time and mock behavior

Network Event date/time inputs use the browser timezone and send RFC3339 UTC.
Its filters, offset and limit stay in the URL. Relative time windows must be
anchored when applied, not when the page mounted.

`web/mocks/admin.ts` is a typed development API. Keep every implemented route in
sync with the real method and response shape. Placeholder Overview sparklines
are intentionally retained and visibly labelled; do not describe them as live
until historical telemetry exists.

## Verification

```bash
npm --prefix web run lint
npm --prefix web test
npm --prefix web run build
npm --prefix web run test:e2e
```

Playwright discovers Chrome on macOS, Linux, and Windows, then falls back to its
bundled Chromium. Prefer geometry and computed-style assertions over screenshot
judgment. `refs/kumo/` is reference-only and must never be imported.
