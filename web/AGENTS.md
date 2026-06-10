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
* **TanStack Router** for routing (code-based — see below), **TanStack Query**
  for server state, **TanStack Table** for grids.
* **Zustand** for the little client-side state there is (auth session).
* **Radix UI** primitives, wrapped in-house under `src/shared/ui/`.
* **CSS Modules + design tokens** — no Tailwind, no utility CSS.
* Chart.js (via `react-chartjs-2`), `react-hook-form`, `react-day-picker`
  (date-range calendar), `date-fns`, `yaml`, `jwt-decode` (token claims),
  `@dnd-kit/*` (drag-reorder, e.g. the rules tree), `diff` (the rules diff view).

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
├── main.tsx          # entry: mounts the router, loads styles/base.css
├── app/              # router.tsx (route tree + the QueryClient + providers);
│                     #   layout/ (AppShell, Sidebar, Topbar, CommandPalette, HowToMenu)
├── features/         # one folder per feature: page + api.ts hooks + types
│   ├── alerts/ rules/ snoozes/ notifications/ dashboard/ auth/ audit/ dev/
│   └── admin/        #   users, roles, environments, widgets, kv, settings, status, tenants
├── shared/           # cross-feature: ui/ (Radix wrappers), forms/, chart/, condition/,
│                     #   searchdsl/, hooks/, icons/, auth/ (RequirePerm), modifications/
├── lib/              # non-component logic: api/, auth/, condition/, format/, timeconstraints/
├── styles/           # base.css + tokens.css + theme.{dark,light}.css
└── tests/            # Vitest setup + MSW server + global a11y audit (NOT e2e — see below)
```

> `app/layout/AppShell.tsx` is the layout; the `QueryClient` is instantiated
> inline in `router.tsx` (there is no standalone file for it). `features/dev/`
> is a developer-only showroom (`PrimitivesPage` UI gallery + `ResourcePage`
> demo): route-wired at `/web/dev/*` but unlinked from nav, and built **only
> under `import.meta.env.DEV`** — Rollup drops the routes and modules from
> production bundles; keep new dev-only routes inside that gate.
> `features/auth/` owns the login flow: `Login.tsx` renders one button per
> backend descriptor (form-first + SSO via `parseBackends()`/`ssoStartUrl()` in
> its `api.ts`), and `LoginCallback.tsx` handles the OIDC return at
> `/web/login/callback`.

| You're adding…                         | Put it in…                                                      |
|----------------------------------------|-----------------------------------------------------------------|
| A new page / feature view              | `src/features/<feature>/` (page component + `api.ts` Query hooks) |
| A reusable UI primitive                | `src/shared/ui/` (wrap Radix, co-locate `<Name>.module.css`)    |
| An API call                            | a TanStack Query hook over `api()` from `@/lib/api`; **regen `types.gen.ts` if the contract changed** |
| A route                                | register it in `src/app/router.tsx` (code-based — there is **no** `routeTree.gen.ts`) |
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
* Style with CSS Modules + the custom properties in `styles/tokens.css` (so
  dark/light themes keep working) — don't hard-code colors.

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
(`sqlite` default | `postgres` | `mongo`) and keeps a committed screenshot
baseline. Run it when a change touches a critical user flow.

---

## Don't

* Hand-edit `src/lib/api/types.gen.ts`, or any compiled config artifact
  (`vite.config.js`/`.d.ts`, `tsconfig.tsbuildinfo`) — they're generated/ignored.
* Let the SPA and `../api/openapi.yaml` drift — regenerate instead.
* Reach for Tailwind, a second state library, or file-based routing; match the
  patterns already in `features/` and `shared/ui/`.
* Casually edit the `manualChunks` vendor-splitting in `vite.config.ts` — the
  ordering is load-bearing (its comments record real prod failures: react-table
  must match before the generic react check; Radix/floating-ui must co-locate;
  scheduler/use-sync-external-store must land in vendor-react). Reordering can
  break startup in headless Chromium.
