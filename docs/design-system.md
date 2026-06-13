# Design System

Visual and layout conventions for the BoxFleet admin UI. The UI is built on
**native Cloudflare Kumo** components and Kumo's **semantic tokens**. When in
doubt, consult the source of truth: `npx kumo ai` (usage guide) and
`npx kumo docs <Component>` or `npx kumo doc <Component>` (per-component props).

## Foundation

All visual values come from Cloudflare Kumo. `web/src/styles/globals.css` keeps
this exact order:

```css
@source "../../node_modules/@cloudflare/kumo/dist/**/*.{js,jsx,ts,tsx}";
@import "@cloudflare/kumo/styles";  /* registers Kumo @theme tokens first */
@import "tailwindcss";              /* Tailwind utilities */
```

Two hard rules from `kumo ai`:

1. **Semantic tokens only.** Use `bg-kumo-base`, `text-kumo-default`,
   `border-kumo-hairline`, etc. Never raw Tailwind colors (`bg-blue-500`) and
   never hardcoded hex/HSL in components.
2. **No `dark:` variant.** Light/dark is handled automatically via CSS
   `light-dark()`. Never add `dark:` prefixes.

> The legacy Geist `--ds-*` palette and shadcn-shaped `components/ui/`
> compatibility layer have been removed. Do not add new `--ds-*`, `gray-1000`,
> `blue-700`, or raw color utility classes — they are not Kumo semantic tokens.

## Color (semantic tokens)

### Surface hierarchy

| Token | Purpose |
|-------|---------|
| `bg-kumo-canvas`   | Outermost page background, behind everything |
| `bg-kumo-base`     | Default component / panel background |
| `bg-kumo-elevated` | Slightly raised surface (e.g. `LayerCard.Secondary`) |
| `bg-kumo-recessed` | Recessed fill (e.g. segmented `Tabs` track) |
| `bg-kumo-tint`     | Subtle tint for table zebra / hover |
| `bg-kumo-contrast` | High-contrast inverted background |

### Text

| Token | Purpose |
|-------|---------|
| `text-kumo-default`     | Primary body text |
| `text-kumo-strong`      | Slightly less contrast than default |
| `text-kumo-subtle`      | Muted: descriptions, captions, secondary labels |
| `text-kumo-inactive`    | Disabled / inactive |
| `text-kumo-placeholder` | Input placeholders |
| `text-kumo-inverse`     | Text on inverted/contrast backgrounds |
| `text-kumo-link`        | Links |

### Borders

| Token | Purpose |
|-------|---------|
| `kumo-hairline` | Divider between flat surfaces with no shadow (e.g. `LayerCard`) |
| `kumo-line`     | Thicker edge of an elevated surface, paired with a shadow |

### Accent / brand

The single accent is Kumo's **brand** token (`bg-kumo-brand`, `text-kumo-brand`),
which is theme-driven — do **not** hardcode the colour. Primary actions use the
Kumo `Button` `primary` variant (it already renders the brand fill); links use
`text-kumo-link`. Do not reintroduce teal/green as accents.

### Status tones (semantic only)

Status colours communicate state, not brand. Use them on dots, icons, badges,
and meter fills — never as decorative chrome.

| State                   | Token            |
|-------------------------|------------------|
| Info / neutral          | `kumo-info`      |
| Success / online / OK   | `kumo-success`   |
| Warning / pending       | `kumo-warning`   |
| Error / offline         | `kumo-danger`    |

## Typography

Prefer the Kumo `Text` component (`variant`: `heading1/2/3`, `body`, `secondary`,
`success`, `error`, `mono`; `size`: `xs/sm/base/lg`; polymorphic via `as`) over
hand-styled headings.

| Use             | Approach |
|-----------------|----------|
| Page title      | `AppPageHeader` title (or Kumo `Text variant="heading1"` inside content) |
| Section heading | `Text variant="heading3"` / `heading2` |
| Body            | `Text variant="body"` (`text-kumo-default`) |
| Caption / label | `Text variant="secondary" size="sm"` (`text-kumo-subtle`) |

## App-shell layout

Use Kumo's `Sidebar` for the shell, not a hand-rolled `<aside>`:

```
Sidebar.Provider
└─ Sidebar
   ├─ Sidebar.Header   app name + logo icon
   ├─ Sidebar.Content  Sidebar.Menu / Sidebar.MenuButton (active = current route)
   └─ Sidebar.Footer   Sidebar.Trigger
main                    AppPageHeader (breadcrumbs + actions + title) + page content
```

- Navigation pages are defined in `web/src/navigation.ts`. Add a page there
  first, then add its `<Route>` in `App.tsx`.
- Page titles + page-level action buttons use `AppPageHeader`
  (`web/src/components/app-page-header.tsx`). Render it inside each page so the
  page owns its title and actions.
- The `AppPageHeader` top breadcrumb/action bar is intentionally `h-[58px]` with
  `border-b border-kumo-line` so it aligns with Kumo's `Sidebar.Header`
  (`h-[58px]` in the upstream Sidebar source). Preserve that alignment; it makes
  the sidebar/header divider read as one continuous line.
- The shell uses `Sidebar.Provider className="h-svh bg-kumo-canvas"` and
  `<main className="min-w-0 flex-1 overflow-y-auto">`. Do not replace this with
  `min-h-svh` + child `h-full`; percentage heights do not resolve against
  min-height and the sidebar footer/trigger will drift.

## Component usage rules

1. **Native Kumo first.** Import from `@cloudflare/kumo`. Hand-roll only when
   Kumo has no equivalent; reach for `@cloudflare/kumo/primitives/*` (Base UI)
   before writing raw markup.
2. **Layout** with `Grid` (`variant="4up"`, `"2-1"`, …), `Surface`
   (`flat`/`raised`), and `LayerCard`. KPI/metric tiles are `Surface`
   (`rounded-lg p-4`) inside a `Grid`.
3. **Data tables** use the Kumo `Table` compound (`Table.Header/Head/Body/Row/Cell`).
   Paginated/filterable tables add TanStack Table + server-side query params
   (see Network Events).
4. **Forms**: native Kumo `Input` / `Select` / `Switch` / `Field`. Never a native
   `<select>`. Empty states use Kumo `Empty`; alerts use `Banner`.
5. **Icons**: `@phosphor-icons/react` for app icons (matches Kumo internals). Do
   not mix `lucide-react` into new code.
6. **Tokens & shadows**: semantic tokens only (see Color). Use Kumo's own shadow
   utilities, not arbitrary `shadow-*` values. No `dark:` variants.

## Kumo implementation notes

- Kumo blocks are vendored source, not importable dependencies. `kumo add` writes
  blocks under `web/kumo.json`'s `blocksDir` (`src/components/kumo`), after which
  BoxFleet may adapt the copied source.
- The vendored `web/src/components/kumo/page-header/page-header.tsx` is a
  reference copy of Kumo's `PageHeader` block. The active app header is
  `AppPageHeader`, because BoxFleet needs the 58px sidebar-aligned top bar and a
  stable right-side actions slot for future review/publish controls.
- Use `@phosphor-icons/react`; Kumo uses Phosphor internally.
- Inter is loaded through `@fontsource-variable/inter` and `--font-sans:
  "Inter Variable", ...` in `globals.css`. Kumo's compact line heights assume
  Inter; falling back to a different system font can clip descenders such as `g`
  and `y`.
- Kumo `Text` is a discriminated TypeScript union. `mono-*` variants do not accept
  every `size` accepted by body text; check `npx kumo docs Text` before changing
  text variants.
- Kumo square icon buttons require an accessible label. When using square
  buttons, include `aria-label`.
- Avoid `Sidebar.Rail` unless a current Kumo example specifically calls for it;
  the app currently uses a bottom `Sidebar.Trigger`, matching the desired
  Cloudflare-style collapsed sidebar.

## Visual verification

For layout-sensitive changes, inspect the rendered UI with Playwright and system
Chrome:

```ts
chromium.launch({ executablePath: "/usr/bin/google-chrome-stable" })
```

Prefer measuring actual DOM rectangles and computed styles over judging from
memory. Useful checks include page-header/sidebar-header height and border
alignment, font loading via `document.fonts.check(...)`, and mobile/desktop
overflow. Cloudflare Kumo examples live in `refs/kumo/packages/kumo-docs-astro/`;
component source lives in `refs/kumo/packages/kumo/src/`.

## UX preferences (durable, from prior review)

These reflect decisions the user made on earlier iterations and still apply:

- **Restraint over chrome.** Vercel/Tailscale feel: status shown as a coloured
  dot + plain text, not chunky pills in table rows.
- **Single-column forms.** Do not put two config fields on one row. Short fields
  are narrowed (don't stretch every `Input` to 100%); multi-line / JSON fields
  stay full-width.
- **Terse copy.** Status cards and hints are short; drop explanatory filler.
- **Loading affordance goes after the text** ("正在等待节点连接控制面 ●●●",
  not before).
- **No subtitles stacked under a primary name** in tables. Use a separate column
  (primary value + small secondary) instead.
- **Soft-delete preferred** (status = disabled) over hard delete; a "show
  disabled" toggle filters them out by default.
- **HMR workflow.** `npm --prefix web run dev` for live editing; don't require a
  Go server restart for CSS/markup tweaks.
