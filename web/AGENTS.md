# AGENTS.md — web frontend

> Scope: the React SPA under `web/`. For repo-wide rules, the Go backend, and
> architecture, read `../AGENTS.md` first — it wins on any conflict. This file
> only covers **working in the SPA**.

Root hard rule #4 applies here: frontend changes go through the standard
PR/CI flow, and **changes under `web/` are in the "confirm first" bucket** —
check with the user before reworking UI.

---

## Stack

* React 19 + Vite 6, **TypeScript strict**. Node ≥ 22.
* **TanStack Router** for routing (code-based — see below; pages are
  lazy-loaded route chunks), **TanStack Query** for server state. Grids are
  the in-house `shared/ui/DataTable` — there is **no** table library.
* **Zustand** for the little client-side state there is (auth session).
  There is **zero React Context** in the SPA — don't introduce one for form
  state either; react-hook-form is the form-state provider.
* **Radix UI** primitives, wrapped in-house under `src/shared/ui/`.
* **CSS Modules + design tokens** — no Tailwind, no utility CSS.
* **IBM Plex Sans Variable + IBM Plex Mono**, self-hosted via
  `@fontsource-variable/ibm-plex-sans` / `@fontsource/ibm-plex-mono` and
  imported in `main.tsx` before the base styles (the primary latin woff2 is
  preloaded by an inline plugin in `vite.config.ts`).
* Chart.js (wrapped directly in `shared/chart/` — no react-chartjs-2),
  `react-hook-form`, `react-day-picker` (date-range calendar), `date-fns`,
  `yaml` (dynamic-imported in copy handlers), `jwt-decode` (token claims),
  `@dnd-kit/*` (drag-reorder, e.g. the rules tree), `diff` (the rules diff view).

---

## Design language

"Mission control" register: near-black warm-neutral surfaces with one amber
signal accent. The shell owns scrolling — `body` never scrolls (the layered
canvas texture paints once); `main` scrolls internally.

* **Tokens only.** Components read `var(--token-name)` — never hex/px
  literals. Color tokens live in `styles/theme.{dark,light}.css`;
  `styles/tokens.css` is theme-independent (space, radius, type, motion,
  shadow, z-index, fonts).
* **Dark is the default.** `theme.dark.css` sits on `:root`;
  `theme.light.css` overrides via `[data-theme="light"]`, toggled from the
  Topbar (`useTheme()`) — a hard-coded color breaks one of the two themes.
* **Amber = interactive chrome only.** `--accent` (#ffb000) drives buttons,
  links, active nav, focus. Never use it for severity; blue is reserved
  exclusively for `--severity-info`, so accent and severity never collide.
* **Timestamps**: render epoch-second values with `shared/ui/TimeCell` —
  semantic `<time>` + full-locale tooltip + relative "Nm ago" prefix while
  under an hour old, in mono tabular figures (`--font-features-numeric`).
  The underlying helpers (`formatRelativeTime`, `trimDate`) live in
  `lib/format/time.ts`.
* **Charts**: Chart.js paints to canvas and can't resolve `var(--x)` — take
  colors from `shared/chart/theme.ts` (`chartToken()` resolves tokens via
  `getComputedStyle`, with jsdom/SSR fallbacks) so charts re-theme with
  light/dark. `DistributionBar` is the pure-CSS distribution strip (there is
  no donut chart).
* **Responsive / mobile.** Breakpoint tokens `--bp-sm/md/lg` (plus
  `--touch-target`, `--safe-bottom`) live in `tokens.css`. The shell swaps from
  the desktop sidebar to the touch layout (bottom-tab `BottomNav` + `MoreSheet`)
  below `--bp-lg` (1024px) via the `useIsMobileShell()` hook — the **only** JS
  layout fork. Everywhere else, adapt with **container queries** (a primitive
  reacts to its own container — e.g. `DataTable` collapses rows into cards at a
  ≤640px container) and `@media (pointer: coarse)` for touch sizing, never
  viewport media queries scattered per page. Desktop output (≥`--bp-lg`) must
  stay byte-for-byte unchanged.

---

## The generated boundary (read this first)

`src/lib/api/types.gen.ts` is **generated from `../api/openapi.yaml`** by
`openapi-typescript` and is **committed**. It carries an "auto-generated — do
not edit" header. So:

* **Never hand-edit `types.gen.ts`.** Change `../api/openapi.yaml`, then
  `npm run codegen`, then commit the regenerated file.
* The OpenAPI contract is the single source of truth (root rule). The Go
  backend and this client both derive from it — they must not drift.
* Everything else under `src/lib/api/` is hand-written: `client.ts` (the
  `api<T>(method, path, opts)` fetch wrapper — token attach, 401-refresh,
  `ApiError`), `resource.ts` (`defineResource()` query-key factory).

---

## Where each kind of change goes

```
web/src/
├── main.tsx          # entry: mounts the router, loads fonts then styles/base.css
├── app/              # router.tsx (route tree + the QueryClient + providers);
│                     #   layout/ (AppShell, Sidebar, Topbar, CommandPalette,
│                     #            HowToMenu, nav-items.ts, breadcrumb.ts)
├── features/         # one folder per feature: page + api.ts hooks + types
│   ├── alerts/ rules/ snoozes/ notifications/ dashboard/ auth/ audit/ dev/
│   └── admin/        #   users, roles, environments, widgets, kv, settings, status, tenants
├── shared/           # cross-feature: ui/ (Radix wrappers + TimeCell), forms/,
│                     #   chart/ (theme.ts + Chart.js wrappers), condition/,
│                     #   searchdsl/, hooks/, icons/, auth/ (RequirePerm), modifications/
├── lib/              # non-component logic: api/, auth/, condition/, format/, timeconstraints/
├── styles/           # base.css + tokens.css + theme.dark.css (default) + theme.light.css
└── tests/            # Vitest setup + MSW server + global a11y audit (NOT e2e — see below)
```

> `app/layout/AppShell.tsx` is the layout; the `QueryClient` is instantiated
> inline in `router.tsx` (there is no standalone file for it).
> `app/layout/breadcrumb.ts` (`pickBreadcrumb()`) maps router matches to the
> nav group + label the Topbar renders. `features/dev/` is a developer-only
> showroom (`PrimitivesPage` UI gallery + `ResourcePage` demo): route-wired at
> `/web/dev/*` but unlinked from nav, and built **only under
> `import.meta.env.DEV`** — Rollup drops the routes and modules from
> production bundles; keep new dev-only routes inside that gate.
> `features/auth/` owns the login flow: `Login.tsx` renders one button per
> backend descriptor (form-first + SSO via `parseBackends()`/`ssoStartUrl()` in
> its `api.ts`), and `LoginCallback.tsx` handles the OIDC return at
> `/web/login/callback`.

> Page anatomy: `features/alerts/` composes lifecycle tabs (`tabs.ts`), the
> `EnvironmentBar`, the `ActiveFilters` chip strip (one dismissable chip per
> active constraint) and hover-revealed inline row actions (ack/close without
> the dialog). `features/dashboard/` is `StatTiles` + charts behind a
> `DashboardSkeleton`, driven by `TimeRangePicker`/`time-range.ts`.
> `features/admin/` covers user management (`UserEditor`), role → LDAP/OIDC
> group mapping (`RoleEditor`), and `settings/`, which renders the backend
> settings catalogue as grouped tabs (general, notifications, ldap, oidc,
> housekeeping) — OIDC/SSO and LDAP are configurable from the web UI.

| You're adding…                         | Put it in…                                                      |
|----------------------------------------|-----------------------------------------------------------------|
| A new page / feature view              | `src/features/<feature>/` (page component + `api.ts` Query hooks) |
| A list page over a resource            | build on `shared/hooks/useResourceListPage` (URL-sync, selection, confirm-delete, context menu) + your own `<DataTable>` — `UsersPage` is the worked example |
| A record editor (create/edit drawer)   | build on `shared/forms/EditorDrawer` (chrome, lifecycle, toasts, dirty-close guard) — `UserEditor` is the worked example; `ActionEditor` is the one deliberate exception (it's a wizard) |
| A reusable UI primitive                | `src/shared/ui/` (wrap Radix, co-locate `<Name>.module.css`)    |
| An API call                            | a TanStack Query hook over `api()` from `@/lib/api`; **regen `types.gen.ts` if the contract changed** |
| A route                                | register it in `src/app/router.tsx` (code-based — there is **no** `routeTree.gen.ts`); pages are `lazyRouteComponent` chunks — keep new pages lazy (the router preloads on intent) |
| Cross-cutting logic (parsing, format)  | `src/lib/<domain>/`                                             |
| Auth / permission logic               | `src/lib/auth/` (Zustand `authStore` + `useAuth()`)             |

---

## Conventions

* Import with the `@/` alias (`@/shared/ui/Button`) → `src/*`. No deep `../../..`.
* Components are PascalCase `.tsx`; hooks/utils are camelCase `.ts`.
* Co-locate the unit test (`Foo.test.tsx`) and styles (`Foo.module.css`) next
  to the component.
* Server state lives in TanStack Query (invalidate on mutation success), not in
  component state or Zustand. Zustand is for the auth session only.
* **Shareable UI state lives in the URL** — tabs, filters, pagination, the
  dashboard time range all round-trip through typed route search params
  (`validateSearch` in `router.tsx`), never in bare `useState`.
* React 19: `ref` is a regular prop — **no `forwardRef`** (the whole codebase
  was converted; don't reintroduce it).
* **Never `window.confirm`** — Playwright auto-dismisses it, so guarded flows
  silently break in e2e. Use an in-DOM dialog (`EditorDrawer`'s discard guard
  is the pattern).
* Function props that reach `DataTable` (and memoized rows generally) must be
  identity-stable — `useCallback`/`useMemo` or the `useResourceListPage`
  return values, never fresh inline closures per render.
* Style with CSS Modules + the design tokens (see **Design language**) — don't
  hard-code colors.

---

## Run & verify

```bash
task web:install        # npm ci (once)
task web:dev            # Vite dev server on :5173, proxies /api → backend :5200
```

Before calling a change done, the full check set (root hard rule #4) must pass:

```bash
task web:lint           # eslint
task web:typecheck      # tsc --noEmit (strict)
task web:test           # vitest run (unit + a11y, MSW-mocked)
task web:format:check   # prettier --check
task web:build          # tsc -b && vite build → web/dist/ (shipped by snooze-server)
```

`npm run codegen` (no `task` wrapper) regenerates `types.gen.ts` from the spec.
`task web:preview` serves a built bundle on :4173; `task web:test:watch` is the
watch-mode runner.

E2E (Playwright) lives in the **top-level `web/tests/e2e/`** (not `src/tests/`).
First build the harness with `npm run e2e:build` (runs `vite build` **and**
`go build ./cmd/snooze-server` into `tests/e2e/.bin/`), then `npm run e2e`
(or `e2e:headed`) — plain `e2e` does not rebuild the server. The harness boots a
real server against a pluggable DB chosen by `SNOOZE_TEST_DB`
(`sqlite` default | `postgres` | `mongo`). Functional specs live under domain
directories; run them when a change touches a critical user flow. A separate
visual tour (`tests/e2e/tour.spec.ts`) seeds varied data, walks every
top-level route and screenshots each into `tests/e2e/.screenshots/`
(gitignored); it is skipped by default — opt in with `SNOOZE_TOUR=1`.

---

## Don't

* Hand-edit `src/lib/api/types.gen.ts`, or any compiled config artifact
  (`vite.config.js`/`.d.ts`, `tsconfig.tsbuildinfo`) — they're generated/ignored.
  Worse: a stale `vite.config.js` left in `web/` **silently shadows
  `vite.config.ts`** (Vite resolves `.js` first) — if config changes don't
  take effect, delete it.
* Let the SPA and `../api/openapi.yaml` drift — regenerate instead.
* Reach for Tailwind, a second state library, or file-based routing; match the
  patterns already in `features/` and `shared/ui/`.
* Use `--accent` for severity, or hard-code colors anywhere — tokens only.
* Casually edit the `manualChunks` vendor-splitting in `vite.config.ts` — the
  ordering is load-bearing (its comments record real prod failures: any package
  whose name contains "react" — react-table, **react-day-picker** — must match
  before the generic react check; Radix/floating-ui must co-locate;
  scheduler/use-sync-external-store must land in vendor-react). Reordering can
  break startup in headless Chromium.
