# Web UI Refactor — Handover

In-progress refactor of `web/` from hand-written CSS to **Tailwind v3 + shadcn/Radix primitives + Vercel `vercel-ui` aesthetic** (Geist color palette, dot+text status, kebab row actions, dark-themed snippet, etc.).

Current app-level libraries also include TanStack Query, TanStack Table,
react-hook-form, zod, date-fns, and react-day-picker. Network Events is the
first complete page using that stack end to end.

Read this together with `CLAUDE.md`. Everything below is what the previous agent left for you to continue.

---

## 1. What's done

### Foundation
- Installed: `tailwindcss@3`, `postcss`, `autoprefixer`, `tailwindcss-animate`, `class-variance-authority`, `clsx`, `tailwind-merge`, `@radix-ui/react-{dialog,tabs,tooltip,slot,label,switch,dropdown-menu}`.
- `web/tailwind.config.cjs` — full Geist palette (`gray`, `gray-alpha`, `blue`, `red`, `amber`, `green`, `teal`, `purple`, `pink` each at 100–1000), shadow tokens, copy/spinner/loading-dots keyframes. **Preflight is ON** (was off during early migration; re-enabled in this round once old CSS was gone).
- `web/postcss.config.cjs` standard.
- Path alias `@/*` → `src/*` (wired in `tsconfig.json` and `vite.config.ts`).
- `web/src/styles/globals.css` — `:root` CSS vars (full palette + shadows). No `.dark` block yet.
- `web/src/styles.css` — trimmed to ~23 lines (font, body bg, box-sizing). Old 944 lines of layout/component CSS are deleted.
- `web/src/lib/utils.ts` — `cn()` helper.
- `web/vite.config.ts` proxy `/api → 127.0.0.1:18081` (not 8080 — matches the dev server addr).

### Primitives (`web/src/components/ui/`)
| File | Notes |
|---|---|
| `button.tsx` | cva variants `default/secondary/tertiary/error/warning`, sizes `tiny/sm/md/lg`, `prefix`/`suffix`/`loading`/`svgOnly`/`asChild`. |
| `input.tsx` | `prefix`/`suffix`, `label`, `containerClassName` (use for width constraints), focus ring via `shadow-input-ring`. |
| `textarea.tsx` | matches Input styling. |
| `select.tsx` | native `<select>` styled with chevron, same shape as Input. |
| `label.tsx` | Radix label. |
| `dialog.tsx` | Radix dialog. **Important**: has `overflow-x-hidden` + `[&>*]:min-w-0` to prevent intrinsic-width children from blowing out the modal. Sizes `md/lg/xl`. |
| `dropdown-menu.tsx` | Radix dropdown for row-action kebab menus. Items support `destructive` flag. |
| `tabs.tsx` | Radix tabs. Unused so far — kept for future. |
| `switch.tsx` | Radix switch, dark-on-light. Used in NodesPage "Show disabled" and Proxy Enabled. |
| `table.tsx` | shadcn-style primitives (`Table`/`TableHeader`/`TableBody`/`TableRow`/`TableHead`/`TableCell`). Wrapped in `overflow-auto` div. |
| `card.tsx` | `Card`/`CardHeader`/`CardTitle`/`CardContent`/`CardFooter`. Used as the new `Panel`. |
| `badge.tsx` | vercel-ui colored chip — 16 variants (`gray`/`blue`/`green`/`teal`/`amber`/`red`/`purple`/`pink` × solid + `-subtle`). **Not currently used in tables** — status cells use plain dot+text. Reserve for real chips. |
| `status-dot.tsx` | colored dot with optional `pulse`. |
| `snippet.tsx` | code block with animated copy button. Variants `default/dark/success/error/warning`. **Has `wrap` prop**: when true, long content folds (`whitespace-pre-wrap break-all`); when false, horizontal scroll. AddNodeModal uses `<Snippet variant="dark" wrap>` for the bootstrap string. |
| `note.tsx` | inline callout (`secondary/success/error/warning/violet/cyan`), supports `fill` + `action`. Used for alerts, empty states, status cards. |
| `loading-dots.tsx` | three blinking dots. |
| `spinner.tsx` | 12-segment Geist spinner. |
| `kbd.tsx` | keyboard chip — not yet used. |
| `gauge.tsx` | Geist gauge — not yet used. |

### Application files
- **`web/src/components.tsx`** (~290 lines) — fully rewritten:
  - All 5 tables (`NodesTable` / `ProxiesTable` / `NetworkEventsTable` / `UsersTable` / `UserAccessTable`) use new `Table` primitives.
  - Row click is **removed** — replaced with a kebab actions column at the row end via `ActionsCell` helper. Currently only shows "Edit". Pattern is set up to easily add Delete / Publish etc.
  - `Panel` is now a thin wrapper over `Card` + `CardHeader` + `CardTitle`.
  - `Metric` uses `Card` with icon + label + value.
  - `StatusBadge` is **dot + text + capitalize** (Vercel/Tailscale style — no pill background). Tones: active/applied/connect/ok → green; pending/unknown/event → amber; disabled → gray; else red.
  - `EmptyState` renders `<Note variant="secondary" size="sm">` inside `p-4` padding.

- **`web/src/pages.tsx`** — fully rewritten with new primitives.
  - `OverviewPage`: grid of `Metric` cards + two `Panel` sections.
  - `NodesPage`: kebab actions, `Switch` for Show-disabled toggle, AddNode + EditNode modals.
  - `AddNodeModal`: two-step (form → generated). Generated step uses `<Snippet variant="dark" wrap>` for the bootstrap string, `<Note variant="secondary" fill>` for the status card. Status card uses `LoadingDots` placed AFTER the text ("正在等待节点连接控制面 ●●●"); explanatory hints were removed per user request.
  - `EditNodeModal`: 2-column grid (form left, live config preview right). Right side is a `<pre>` with `bg-gray-1000`. Footer has 2-stage Delete confirmation + Save.
  - `ProxiesPage`: modal form with **all fields single-column**, short fields use `FieldRow narrow`, JSON textareas full-width.
  - `UsersPage`, `TrafficPage`, `SystemLogsPage`: straightforward `Panel` + table.
  - `NetworkEventsPage`: TanStack Query + TanStack Table + react-hook-form/zod filters + react-day-picker/date-fns date range.
  - **`FieldRow` helper** at the bottom — accepts `label`/`required`/`hint`/`hintTone`/`narrow`/`children`. `narrow` applies `max-w-xs` (≈320px) to the label container. Use this for any new form field.

- **`web/src/App.tsx`** — sidebar/topbar fully Tailwind. Token input uses `<Input type="password">`. Refresh button uses `<Button svgOnly variant="secondary">` with spinning icon. Initial loading uses `<LoadingDots>`. Errors use `<Note variant="error">`.

### Bundle (after this round)
- CSS ~39 kB / 8 kB gzip
- JS ~734 kB / 218 kB gzip
- Vite warns about chunk size. This is a code-splitting/manualChunks follow-up,
  not a reason to remove the established libraries.

---

## 2. Conventions / rules

- **Forms**: every field is a `FieldRow`. Short fields get `narrow`. Multi-line / JSON / structured fields stay full-width.
- **Modals**: always use `<Dialog open onOpenChange={open => !open && onClose()}>` + `DialogContent size="md|lg|xl"`. Don't reach for the old `<Modal>` wrapper (still re-exported nowhere — fully removed).
- **Status text**: use `StatusBadge` from `components.tsx`. If you need a chip with a count (think "5/6"), use `<Badge>` from `ui/badge.tsx` directly.
- **Tables**: use the primitives directly when building a new table, or extend the existing 5 in `components.tsx`. Action column convention: last column, `w-10`, `<ActionsCell label onEdit>`. Add more menu items via `<DropdownMenuItem>` inside the dropdown content.
- **Paginated/filterable tables**: use TanStack Table plus server-side query
  params, following `NetworkEventsPage`.
- **Request state**: use TanStack Query. Avoid new page-level ad hoc
  `loading`/`error` fetch state unless the interaction is truly local.
- **Forms**: new complex forms should use react-hook-form + zod. `FieldRow`
  remains the layout helper.
- **Date/range UI**: use react-day-picker in a Radix Popover and date-fns for
  formatting/conversion.
- **Selects**: use the Radix-backed `Select` primitive. Do not reintroduce
  native `<select>` for app controls.
- **Alerts / notices / empty states**: use `<Note>`, never hand-roll.
- **Long copy targets** (URLs, tokens, bootstrap strings, commands): use `<Snippet wrap>` for blocks, plain `<code>` for inline.
- **Wait states**: short button-level → `loading` prop on `<Button>`. Page-level → `<LoadingDots>`. Inline icons → `<RefreshCw className={spinning ? "animate-spin" : ""}>`.
- **Color usage**: prefer the Geist named scales (`text-gray-700` etc.). Don't introduce one-off hex.
- **Don't** introduce `dark:` variants yet — dark mode CSS vars haven't been ported. User hasn't asked for it.

---

## 3. Known rough spots / not done

| Topic | Status |
|---|---|
| **Dark mode** | `.dark` CSS-var block from vercel-ui is NOT ported. Adding it later is just copying the `.dark` block into `globals.css` and slapping `class="dark"` on `<html>`. |
| **Sidebar accent color** | Active nav item uses neutral `bg-gray-alpha-200`. Old design used `teal-700` as accent. User may want it back — easy swap in `App.tsx`. |
| **Switch color** | Checked state is `bg-gray-1000` (very dark). Could be brighter (blue-700 / teal-700) for more "alive" feel. |
| **Global publish flow** | User asked for it earlier ("global 待发布 button + diff modal + multi-node sync wait"). NOT implemented. Out of scope for this UI refactor — backend diff API doesn't exist yet either. |
| **Row actions = Edit only** | The dropdown menu pattern is set up but only "Edit" is wired. Future: Delete (with `destructive`), Publish, View logs, etc. User's previous attempt at this got pushback for being too minimal — they wanted the kebab affordance, not pricier UX. |
| **Action menu on Proxies / Users** | `ActionsCell` is added to all three big tables (`NodesTable`/`ProxiesTable`/`UsersTable`), but only Nodes was visually validated by the user. Spot-check the others. |
| **Tabs not used** | The `tabs.tsx` primitive exists but nothing uses it. NodesPage could split form / preview / events into tabs if desired. |
| **Bundle splitting** | TanStack/date/form libraries pushed the main JS bundle above Vite's warning threshold. Add route-level lazy loading or `manualChunks` before adding much more UI weight. |
| **Gauge not used** | `gauge.tsx` exists. Could replace some `Metric` cards on OverviewPage with a `Gauge` showing % (e.g., active-nodes ratio). |
| **Mobile responsive** | The new layouts use Tailwind responsive prefixes (`sm:`, `lg:`) but I haven't tested on small screens. The sidebar collapses on `lg:`. |

---

## 4. Dev workflow

```bash
# Terminal 1: Go server on :18081 (embed serves built UI at /admin)
cd /home/haoxin/Projects/BoxFleet
go run ./cmd/boxfleet-server --db /tmp/bf.db --admin-token devtoken --addr 127.0.0.1:18081

# Terminal 2: Vite dev on :5173 with HMR, proxies /api to :18081
cd /home/haoxin/Projects/BoxFleet/web
npm run dev
```

- Live editing: open `http://localhost:5173/admin/`, token `devtoken`. Edits to `web/src/**` hot-reload instantly. No Go restart needed.
- Production path: `http://127.0.0.1:18081/admin/` serves the embedded build. After `npm --prefix web run build`, restart the Go server to pick up new assets (they're `go:embed`-ed in `internal/server/webui/assets/generated/`).
- Pre-commit: `npm --prefix web run build && go test ./... && go vet ./...` (per CLAUDE.md).

---

## 5. User preferences (collected this session)

These are not in memory yet but matter for future iteration on this UI:

- Wants Vercel/Tailscale aesthetic — restrained, colored dot + plain text for status (no chunky pills in table rows).
- Wants single-column forms — explicitly said "不要一行2个配置项".
- Short fields should be narrow (`max-w-xs` is the chosen width). Don't make `<Input>` 100% width by default in forms.
- Status cards / hint text should be terse — removed "Agent 完成粘贴后…" and "上次检查：" lines.
- Loading affordance placement: spinner / dots go AFTER the text, not before ("正在等待节点连接控制面 ●●●", not "●●● 正在等待…").
- Subtitles under primary names in tables = bad. Use a separate column with primary value + small secondary (see the Version column for the pattern).
- Wants `npm run dev` HMR workflow, doesn't want to restart the Go server for every CSS tweak.
- `Save / Publish / Render` confused them — kept Save only, removed Publish from per-node modal, replaced Render with auto-rendered live preview on the right side.
- Soft-delete is preferred (status=disabled) over hard delete; show-disabled toggle filters them out by default.

---

## 6. File locations cheat sheet

```
web/
├── tailwind.config.cjs          # Geist palette + shadows + keyframes
├── postcss.config.cjs
├── vite.config.ts                # @ alias, /api proxy → :18081
├── tsconfig.json                 # @/* path
├── src/
│   ├── main.tsx                  # imports globals.css before styles.css
│   ├── App.tsx                   # shell (sidebar + topbar)
│   ├── pages.tsx                 # 7 page components + AddNode/EditNode modals
│   ├── components.tsx            # 5 tables + Panel + Metric + StatusBadge + EmptyState
│   ├── navigation.ts             # sidebar nav items
│   ├── types.ts                  # API types (DON'T edit casually — mirrors Go db facade)
│   ├── utils.ts                  # shared formatting helpers
│   ├── styles.css                # body font/bg/reset only (~23 lines)
│   ├── styles/globals.css        # Tailwind layers + Geist CSS vars
│   ├── lib/utils.ts              # cn() helper
│   └── components/ui/            # shadcn/Radix-style primitives
docs/
└── web-ui-refactor.md            # this file
```

---

## 7. If you need to keep going

Likely next requests from the user:

1. **Polish pass on Proxies / Users / Overview pages** — they validated Nodes most thoroughly; the others may need similar attention.
2. **Visual tuning**: sidebar accent color, switch color, modal sizing for different pages.
3. **More row actions**: Delete on Nodes, Disable/Enable on Proxies, View Logs on any of them.
4. **Global publish button** — see Known rough spots. Needs backend diff API first.
5. **Dark mode** — straightforward when wanted.

When making changes, follow the conventions in §2. The user reviews visually after each change and likes incremental + visible diffs.
