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
* Chart.js (via `react-chartjs-2`), `react-hook-form`, `date-fns`, `yaml`.

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
├── app/              # router.tsx (route tree), AppShell layout, QueryClient
├── features/         # one folder per feature: page + api.ts hooks + types
│   ├── alerts/ rules/ snoozes/ notifications/ dashboard/ auth/ audit/
│   └── admin/        #   users, roles, environments, widgets, kv, settings, status
├── shared/           # cross-feature: ui/ (Radix wrappers), forms/, chart/,
│                     #   condition/, searchdsl/, hooks/, icons/
├── lib/              # non-component logic: api/, auth/, condition/, format/…
├── styles/           # base.css + tokens.css + theme.{dark,light}.css
└── tests/            # Vitest setup + MSW server + global a11y audit
```

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

E2E (Playwright, builds a real `snooze-server` and drives the UI) lives in
`tests/e2e/`: `npm run e2e` (or `e2e:headed`). Run it when a change touches a
critical user flow.

---

## Don't

* Hand-edit `src/lib/api/types.gen.ts`, or any compiled config artifact
  (`vite.config.js`/`.d.ts`, `tsconfig.tsbuildinfo`) — they're generated/ignored.
* Let the SPA and `../api/openapi.yaml` drift — regenerate instead.
* Reach for Tailwind, a second state library, or file-based routing; match the
  patterns already in `features/` and `shared/ui/`.
