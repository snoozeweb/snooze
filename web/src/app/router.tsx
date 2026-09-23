import {
  createRoute,
  createRootRoute,
  createRouter,
  lazyRouteComponent,
  Outlet,
  redirect,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { LiveAnnouncerProvider } from "@/shared/a11y/LiveAnnouncer";
import { Spinner } from "@/shared/ui/Spinner";
import { AppShell } from "./layout/AppShell";
import { firstLandingPath } from "./layout/nav-list";
import { NotFound } from "@/shared/auth/NotFound";
// Dev-only showroom pages stay statically imported: they live behind the
// `import.meta.env.DEV` route block below, which Rollup dead-code-eliminates
// (along with these modules) from production builds. See devRoutes.
import { PrimitivesPage } from "@/features/dev/PrimitivesPage";
import { ResourcePage } from "@/features/dev/ResourcePage";
import { authStore } from "@/lib/auth/store";
// First-paint auth path stays eager so the login screen renders without a
// chunk round-trip. Every other production page is lazy-loaded below via
// lazyRouteComponent so it ships as its own route chunk.
import { Login } from "@/features/auth/Login";
import { LoginCallback } from "@/features/auth/LoginCallback";
import { setUnauthorizedHandler } from "@/lib/api/client";
import { ensureFreshToken, startSessionRefresh } from "@/lib/auth/session";
import { loginRedirectSearch } from "@/lib/auth/return-to";
import { validateAlertsSearch, type AlertsSearchParams } from "./alertsSearch";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Per-resource refetch intervals are wired at the hook level.
      // Globals: don't refetch on window focus (operations tool, not
      // e-commerce) and retry once on network errors only.
      refetchOnWindowFocus: false,
      retry: (failureCount, error) => {
        if (error instanceof Error && error.name === "ApiError") {
          return false;
        }
        return failureCount < 1;
      },
      staleTime: 30_000,
    },
  },
});

const rootRoute = createRootRoute({
  component: () => (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <ToastProvider>
          {/* One pair of live regions for the whole app, mounted above the
              Outlet so every route — and the login screen — can announce
              through the same hook without ever duplicating a region. */}
          <LiveAnnouncerProvider>
            <Outlet />
            <Toaster />
          </LiveAnnouncerProvider>
        </ToastProvider>
      </TooltipProvider>
    </QueryClientProvider>
  ),
});

const webLayoutRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "weblayout",
  component: AppShell,
  // A stray/stale `/web/<unknown>` renders here — inside AppShell's Outlet —
  // so the sidebar/topbar/theme survive instead of a bare "Not Found" on an
  // empty canvas.
  notFoundComponent: NotFound,
  beforeLoad: async ({ location }) => {
    // Don't gate on the store snapshot alone: after a long idle (or a browser
    // reload) the access token can be expired while the refresh token is still
    // perfectly good. Rotating here means an expired tab resumes silently
    // instead of bouncing the operator to the login screen.
    if (await ensureFreshToken()) return;
    // eslint-disable-next-line @typescript-eslint/only-throw-error
    throw redirect({
      to: "/web/login",
      search: loginRedirectSearch(location.href),
    });
  },
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  beforeLoad: () => {
    // eslint-disable-next-line @typescript-eslint/only-throw-error
    throw redirect({ to: firstLandingPath(authStore.getState().claims) });
  },
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/web/login",
  component: Login,
  validateSearch: (search): { return_to?: string; key?: string; sso_error?: string } => {
    const out: { return_to?: string; key?: string; sso_error?: string } = {};
    if (typeof search["return_to"] === "string") out.return_to = search["return_to"];
    if (typeof search["key"] === "string") out.key = search["key"];
    if (typeof search["sso_error"] === "string") out.sso_error = search["sso_error"];
    return out;
  },
});

const loginCallbackRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/web/login/callback",
  component: LoginCallback,
});

const webIndexRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/",
  beforeLoad: () => {
    // eslint-disable-next-line @typescript-eslint/only-throw-error
    throw redirect({ to: firstLandingPath(authStore.getState().claims) });
  },
});

type UsersSearchParams = {
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
  search?: string;
};

const usersRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/admin/users",
  component: lazyRouteComponent(() => import("@/features/admin/users/UsersPage"), "UsersPage"),
  validateSearch: (raw): UsersSearchParams => {
    const out: Record<string, unknown> = {};
    if (typeof raw["uid"] === "string") out["uid"] = raw["uid"];
    const pageRaw = raw["page"];
    const page =
      typeof pageRaw === "number"
        ? pageRaw
        : typeof pageRaw === "string" && /^\d+$/.test(pageRaw)
          ? Number(pageRaw)
          : undefined;
    if (page !== undefined) out["page"] = page;
    if (typeof raw["orderby"] === "string") out["orderby"] = raw["orderby"];
    const ascRaw = raw["asc"];
    const asc =
      typeof ascRaw === "boolean"
        ? ascRaw
        : ascRaw === "true"
          ? true
          : ascRaw === "false"
            ? false
            : undefined;
    if (asc !== undefined) out["asc"] = asc;
    if (typeof raw["search"] === "string") out["search"] = raw["search"];
    return out as UsersSearchParams;
  },
});

type ApiKeysSearchParams = {
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
  search?: string;
};

const apikeysRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/admin/apikeys",
  component: lazyRouteComponent(
    () => import("@/features/account/apikeys/ApiKeysPage"),
    "ApiKeysPage",
  ),
  validateSearch: (raw): ApiKeysSearchParams => {
    const out: Record<string, unknown> = {};
    if (typeof raw["uid"] === "string") out["uid"] = raw["uid"];
    const pageRaw = raw["page"];
    const page =
      typeof pageRaw === "number"
        ? pageRaw
        : typeof pageRaw === "string" && /^\d+$/.test(pageRaw)
          ? Number(pageRaw)
          : undefined;
    if (page !== undefined) out["page"] = page;
    if (typeof raw["orderby"] === "string") out["orderby"] = raw["orderby"];
    const ascRaw = raw["asc"];
    const asc =
      typeof ascRaw === "boolean"
        ? ascRaw
        : ascRaw === "true"
          ? true
          : ascRaw === "false"
            ? false
            : undefined;
    if (asc !== undefined) out["asc"] = asc;
    if (typeof raw["search"] === "string") out["search"] = raw["search"];
    return out as ApiKeysSearchParams;
  },
});

type RolesSearchParams = {
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
  search?: string;
};

const rolesRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/admin/roles",
  component: lazyRouteComponent(() => import("@/features/admin/roles/RolesPage"), "RolesPage"),
  validateSearch: (raw): RolesSearchParams => {
    const out: Record<string, unknown> = {};
    if (typeof raw["uid"] === "string") out["uid"] = raw["uid"];
    const pageRaw = raw["page"];
    const page =
      typeof pageRaw === "number"
        ? pageRaw
        : typeof pageRaw === "string" && /^\d+$/.test(pageRaw)
          ? Number(pageRaw)
          : undefined;
    if (page !== undefined) out["page"] = page;
    if (typeof raw["orderby"] === "string") out["orderby"] = raw["orderby"];
    const ascRaw = raw["asc"];
    const asc =
      typeof ascRaw === "boolean"
        ? ascRaw
        : ascRaw === "true"
          ? true
          : ascRaw === "false"
            ? false
            : undefined;
    if (asc !== undefined) out["asc"] = asc;
    if (typeof raw["search"] === "string") out["search"] = raw["search"];
    return out as RolesSearchParams;
  },
});

type GroupsSearchParams = {
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
  search?: string;
};

const groupsRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/admin/groups",
  component: lazyRouteComponent(() => import("@/features/admin/groups/GroupsPage"), "GroupsPage"),
  validateSearch: (raw): GroupsSearchParams => {
    const out: Record<string, unknown> = {};
    if (typeof raw["uid"] === "string") out["uid"] = raw["uid"];
    const pageRaw = raw["page"];
    const page =
      typeof pageRaw === "number"
        ? pageRaw
        : typeof pageRaw === "string" && /^\d+$/.test(pageRaw)
          ? Number(pageRaw)
          : undefined;
    if (page !== undefined) out["page"] = page;
    if (typeof raw["orderby"] === "string") out["orderby"] = raw["orderby"];
    const ascRaw = raw["asc"];
    const asc =
      typeof ascRaw === "boolean"
        ? ascRaw
        : ascRaw === "true"
          ? true
          : ascRaw === "false"
            ? false
            : undefined;
    if (asc !== undefined) out["asc"] = asc;
    if (typeof raw["search"] === "string") out["search"] = raw["search"];
    return out as GroupsSearchParams;
  },
});

type EnvironmentsSearchParams = {
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
  search?: string;
};

const environmentsRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/admin/environments",
  component: lazyRouteComponent(
    () => import("@/features/admin/environments/EnvironmentsPage"),
    "EnvironmentsPage",
  ),
  validateSearch: (raw): EnvironmentsSearchParams => {
    const out: Record<string, unknown> = {};
    if (typeof raw["uid"] === "string") out["uid"] = raw["uid"];
    const pageRaw = raw["page"];
    const page =
      typeof pageRaw === "number"
        ? pageRaw
        : typeof pageRaw === "string" && /^\d+$/.test(pageRaw)
          ? Number(pageRaw)
          : undefined;
    if (page !== undefined) out["page"] = page;
    if (typeof raw["orderby"] === "string") out["orderby"] = raw["orderby"];
    const ascRaw = raw["asc"];
    const asc =
      typeof ascRaw === "boolean"
        ? ascRaw
        : ascRaw === "true"
          ? true
          : ascRaw === "false"
            ? false
            : undefined;
    if (asc !== undefined) out["asc"] = asc;
    if (typeof raw["search"] === "string") out["search"] = raw["search"];
    return out as EnvironmentsSearchParams;
  },
});

type WidgetsSearchParams = {
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
  search?: string;
};

const widgetsRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/admin/widgets",
  component: lazyRouteComponent(
    () => import("@/features/admin/widgets/WidgetsPage"),
    "WidgetsPage",
  ),
  validateSearch: (raw): WidgetsSearchParams => {
    const out: Record<string, unknown> = {};
    if (typeof raw["uid"] === "string") out["uid"] = raw["uid"];
    const pageRaw = raw["page"];
    const page =
      typeof pageRaw === "number"
        ? pageRaw
        : typeof pageRaw === "string" && /^\d+$/.test(pageRaw)
          ? Number(pageRaw)
          : undefined;
    if (page !== undefined) out["page"] = page;
    if (typeof raw["orderby"] === "string") out["orderby"] = raw["orderby"];
    const ascRaw = raw["asc"];
    const asc =
      typeof ascRaw === "boolean"
        ? ascRaw
        : ascRaw === "true"
          ? true
          : ascRaw === "false"
            ? false
            : undefined;
    if (asc !== undefined) out["asc"] = asc;
    if (typeof raw["search"] === "string") out["search"] = raw["search"];
    return out as WidgetsSearchParams;
  },
});

type KVSearchParams = {
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
  dict?: string;
  search?: string;
};

const kvRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/admin/kv",
  component: lazyRouteComponent(() => import("@/features/admin/kv/KVPage"), "KVPage"),
  validateSearch: (raw): KVSearchParams => {
    const out: Record<string, unknown> = {};
    if (typeof raw["uid"] === "string") out["uid"] = raw["uid"];
    const pageRaw = raw["page"];
    const page =
      typeof pageRaw === "number"
        ? pageRaw
        : typeof pageRaw === "string" && /^\d+$/.test(pageRaw)
          ? Number(pageRaw)
          : undefined;
    if (page !== undefined) out["page"] = page;
    if (typeof raw["orderby"] === "string") out["orderby"] = raw["orderby"];
    const ascRaw = raw["asc"];
    const asc =
      typeof ascRaw === "boolean"
        ? ascRaw
        : ascRaw === "true"
          ? true
          : ascRaw === "false"
            ? false
            : undefined;
    if (asc !== undefined) out["asc"] = asc;
    if (typeof raw["dict"] === "string") out["dict"] = raw["dict"];
    if (typeof raw["search"] === "string") out["search"] = raw["search"];
    return out as KVSearchParams;
  },
});

type SettingsSearchParams = {
  // Active settings tab (a catalogue group key, or "__custom__"). The tab set
  // is catalogue-driven, so route-level validation only enforces "is a
  // string"; SettingsPage falls back to the first real tab when the value
  // doesn't match an available group.
  tab?: string;
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
};

const settingsRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/admin/settings",
  component: lazyRouteComponent(
    () => import("@/features/admin/settings/SettingsPage"),
    "SettingsPage",
  ),
  validateSearch: (raw): SettingsSearchParams => {
    const out: Record<string, unknown> = {};
    if (typeof raw["tab"] === "string") out["tab"] = raw["tab"];
    if (typeof raw["uid"] === "string") out["uid"] = raw["uid"];
    const pageRaw = raw["page"];
    const page =
      typeof pageRaw === "number"
        ? pageRaw
        : typeof pageRaw === "string" && /^\d+$/.test(pageRaw)
          ? Number(pageRaw)
          : undefined;
    if (page !== undefined) out["page"] = page;
    if (typeof raw["orderby"] === "string") out["orderby"] = raw["orderby"];
    const ascRaw = raw["asc"];
    const asc =
      typeof ascRaw === "boolean"
        ? ascRaw
        : ascRaw === "true"
          ? true
          : ascRaw === "false"
            ? false
            : undefined;
    if (asc !== undefined) out["asc"] = asc;
    return out as SettingsSearchParams;
  },
});

type InputsSearchParams = { setup?: string };

const inputsRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/admin/inputs",
  component: lazyRouteComponent(() => import("@/features/admin/inputs/InputsPage"), "InputsPage"),
  validateSearch: (raw): InputsSearchParams => {
    const out: Record<string, unknown> = {};
    if (typeof raw["setup"] === "string") out["setup"] = raw["setup"];
    return out as InputsSearchParams;
  },
});

const statusRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/admin/status",
  component: lazyRouteComponent(() => import("@/features/admin/status/StatusPage"), "StatusPage"),
});

type TenantsSearchParams = {
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
};

const tenantsRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/admin/tenants",
  component: lazyRouteComponent(
    () => import("@/features/admin/tenants/TenantsPage"),
    "TenantsPage",
  ),
  validateSearch: (raw): TenantsSearchParams => {
    const out: Record<string, unknown> = {};
    if (typeof raw["uid"] === "string") out["uid"] = raw["uid"];
    const pageRaw = raw["page"];
    const page =
      typeof pageRaw === "number"
        ? pageRaw
        : typeof pageRaw === "string" && /^\d+$/.test(pageRaw)
          ? Number(pageRaw)
          : undefined;
    if (page !== undefined) out["page"] = page;
    if (typeof raw["orderby"] === "string") out["orderby"] = raw["orderby"];
    const ascRaw = raw["asc"];
    const asc =
      typeof ascRaw === "boolean"
        ? ascRaw
        : ascRaw === "true"
          ? true
          : ascRaw === "false"
            ? false
            : undefined;
    if (asc !== undefined) out["asc"] = asc;
    return out as TenantsSearchParams;
  },
});

type TenantRoutingSearchParams = {
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
};

const tenantRoutingRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/admin/tenant-routing",
  component: lazyRouteComponent(
    () => import("@/features/admin/tenant-routing/TenantRoutingPage"),
    "TenantRoutingPage",
  ),
  validateSearch: (raw): TenantRoutingSearchParams => {
    const out: Record<string, unknown> = {};
    if (typeof raw["uid"] === "string") out["uid"] = raw["uid"];
    const pageRaw = raw["page"];
    const page =
      typeof pageRaw === "number"
        ? pageRaw
        : typeof pageRaw === "string" && /^\d+$/.test(pageRaw)
          ? Number(pageRaw)
          : undefined;
    if (page !== undefined) out["page"] = page;
    if (typeof raw["orderby"] === "string") out["orderby"] = raw["orderby"];
    const ascRaw = raw["asc"];
    const asc =
      typeof ascRaw === "boolean"
        ? ascRaw
        : ascRaw === "true"
          ? true
          : ascRaw === "false"
            ? false
            : undefined;
    if (asc !== undefined) out["asc"] = asc;
    return out as TenantRoutingSearchParams;
  },
});

const alertsRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/alerts",
  component: lazyRouteComponent(() => import("@/features/alerts/AlertsPage"), "AlertsPage"),
  validateSearch: (raw): AlertsSearchParams => validateAlertsSearch(raw),
});

type SnoozesSearchParams = {
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
  search?: string;
  // Prefill for a new snooze, set when arriving via an alert row's "Snooze
  // this alert" action (see AlertsPage). prefillCond is a base64url-encoded
  // Condition, matching the ?q= convention (see encodeConditionQ).
  prefillName?: string;
  prefillComment?: string;
  prefillSeconds?: number;
  prefillCond?: string;
};

const snoozesRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/snoozes",
  component: lazyRouteComponent(() => import("@/features/snoozes/SnoozesPage"), "SnoozesPage"),
  validateSearch: (raw): SnoozesSearchParams => {
    const out: Record<string, unknown> = {};
    if (typeof raw["uid"] === "string") out["uid"] = raw["uid"];
    const pageRaw = raw["page"];
    const page =
      typeof pageRaw === "number"
        ? pageRaw
        : typeof pageRaw === "string" && /^\d+$/.test(pageRaw)
          ? Number(pageRaw)
          : undefined;
    if (page !== undefined) out["page"] = page;
    if (typeof raw["orderby"] === "string") out["orderby"] = raw["orderby"];
    const ascRaw = raw["asc"];
    const asc =
      typeof ascRaw === "boolean"
        ? ascRaw
        : ascRaw === "true"
          ? true
          : ascRaw === "false"
            ? false
            : undefined;
    if (asc !== undefined) out["asc"] = asc;
    if (typeof raw["search"] === "string") out["search"] = raw["search"];
    if (typeof raw["prefillName"] === "string") out["prefillName"] = raw["prefillName"];
    if (typeof raw["prefillComment"] === "string") out["prefillComment"] = raw["prefillComment"];
    if (typeof raw["prefillCond"] === "string") out["prefillCond"] = raw["prefillCond"];
    const prefillSecondsRaw = raw["prefillSeconds"];
    const prefillSeconds =
      typeof prefillSecondsRaw === "number"
        ? prefillSecondsRaw
        : typeof prefillSecondsRaw === "string" && /^\d+$/.test(prefillSecondsRaw)
          ? Number(prefillSecondsRaw)
          : undefined;
    if (prefillSeconds !== undefined) out["prefillSeconds"] = prefillSeconds;
    return out as SnoozesSearchParams;
  },
});

type NotificationsSearchParams = {
  tab?: "notifications" | "actions";
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
  // Per-tab search queries: `search` for the Notifications tab, `actionSearch`
  // for the Actions tab — distinct keys so they don't collide in the URL.
  search?: string;
  actionSearch?: string;
  // Open row-inspector key (a uid) — the docked details drawer is controlled
  // from here so it's shareable and survives a reload, exactly like the alerts
  // page's `?record=`. Namespaced per tab like `search`/`actionSearch` above:
  // the two tabs list different collections, so one shared key would open the
  // Actions drawer on a notification uid (and vice versa) after a tab switch.
  // `details` is also the dashboard deep-link's parameter — don't rename it.
  details?: string;
  actionDetails?: string;
  // Epoch (seconds) window the dashboard deep-link pre-filters the Deliveries
  // tab to; the timeline's window chip clears them.
  from?: number;
  to?: number;
};

const notificationsRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/notifications",
  component: lazyRouteComponent(
    () => import("@/features/notifications/NotificationsPage"),
    "NotificationsPage",
  ),
  validateSearch: (raw): NotificationsSearchParams => {
    const out: Record<string, unknown> = {};
    const tab = typeof raw["tab"] === "string" ? raw["tab"] : undefined;
    if (tab === "notifications" || tab === "actions") out["tab"] = tab;
    if (typeof raw["uid"] === "string") out["uid"] = raw["uid"];
    const pageRaw = raw["page"];
    const page =
      typeof pageRaw === "number"
        ? pageRaw
        : typeof pageRaw === "string" && /^\d+$/.test(pageRaw)
          ? Number(pageRaw)
          : undefined;
    if (page !== undefined) out["page"] = page;
    if (typeof raw["orderby"] === "string") out["orderby"] = raw["orderby"];
    const ascRaw = raw["asc"];
    const asc =
      typeof ascRaw === "boolean"
        ? ascRaw
        : ascRaw === "true"
          ? true
          : ascRaw === "false"
            ? false
            : undefined;
    if (asc !== undefined) out["asc"] = asc;
    if (typeof raw["search"] === "string") out["search"] = raw["search"];
    if (typeof raw["actionSearch"] === "string") out["actionSearch"] = raw["actionSearch"];
    // An empty `?details=` is not "open nothing", it's a key that matches no
    // row — it would render a controlled drawer with no target. Reject it the
    // same way an absent param is rejected.
    if (typeof raw["details"] === "string" && raw["details"] !== "") {
      out["details"] = raw["details"];
    }
    if (typeof raw["actionDetails"] === "string" && raw["actionDetails"] !== "") {
      out["actionDetails"] = raw["actionDetails"];
    }
    // Parsed exactly like `page` above: TanStack hands us either a number or
    // the raw query string depending on how the link was built.
    const epoch = (key: string) => {
      const v = raw[key];
      if (typeof v === "number") return v;
      if (typeof v === "string" && /^\d+$/.test(v)) return Number(v);
      return undefined;
    };
    const from = epoch("from");
    if (from !== undefined) out["from"] = from;
    const to = epoch("to");
    if (to !== undefined) out["to"] = to;
    return out as NotificationsSearchParams;
  },
});

// Dashboard deep-link. `view` picks which of the page's two views is on
// screen; `range` is the time picker's preset key and, for the "custom"
// preset, `from`/`to` carry the window bounds as epoch milliseconds.
// `sort` is the Analyses view's order: omitted means "most urgent", the one
// alternative is `recent` (newest analysis first).
// All optional — no params means the Overview on its default 1d range,
// exactly as before. Types are validated defensively (numeric strings coerced
// to number, an unknown `view` or `sort` dropped) so a hand-edited URL can't
// poison the page.
type DashboardSearchParams = {
  view?: "overview" | "analyses";
  sort?: "recent";
  range?: "1d" | "1w" | "1m" | "1y" | "custom";
  from?: number;
  to?: number;
};

const dashboardRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/dashboard",
  component: lazyRouteComponent(
    () => import("@/features/dashboard/DashboardPage"),
    "DashboardPage",
  ),
  validateSearch: (raw): DashboardSearchParams => {
    const out: Record<string, unknown> = {};
    // Anything but the one named view falls through to the Overview — the
    // param is omitted from the URL in that case, so `?view=overview` and no
    // param at all are the same state.
    if (raw["view"] === "analyses") out["view"] = "analyses";
    // Same rule for the sort: the default ("most urgent") is the absence of
    // the param, so only the one alternative is ever kept.
    if (raw["sort"] === "recent") out["sort"] = "recent";
    const rangeRaw = raw["range"];
    if (
      rangeRaw === "1d" ||
      rangeRaw === "1w" ||
      rangeRaw === "1m" ||
      rangeRaw === "1y" ||
      rangeRaw === "custom"
    ) {
      out["range"] = rangeRaw;
    }
    const num = (k: string) => {
      const v = raw[k];
      if (typeof v === "number" && Number.isFinite(v)) return v;
      if (typeof v === "string" && /^\d+$/.test(v)) return Number(v);
      return undefined;
    };
    const from = num("from");
    if (from !== undefined) out["from"] = from;
    const to = num("to");
    if (to !== undefined) out["to"] = to;
    return out as DashboardSearchParams;
  },
});

const profileRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/profile",
  component: lazyRouteComponent(() => import("@/features/auth/Profile"), "Profile"),
});

type RulesSearchParams = {
  tab?: "rules" | "aggregates" | "reject";
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
  // Per-tab search queries: `search` for the Rules tab, `aggSearch` for the
  // Aggregates tab, `rejSearch` for the Reject tab — distinct keys so they
  // don't collide in the URL.
  search?: string;
  aggSearch?: string;
  rejSearch?: string;
};

const rulesRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/rules",
  component: lazyRouteComponent(() => import("@/features/rules/RulesPage"), "RulesPage"),
  validateSearch: (raw): RulesSearchParams => {
    const out: Record<string, unknown> = {};
    const tab = typeof raw["tab"] === "string" ? raw["tab"] : undefined;
    if (tab === "rules" || tab === "aggregates" || tab === "reject") out["tab"] = tab;
    if (typeof raw["uid"] === "string") out["uid"] = raw["uid"];
    const pageRaw = raw["page"];
    const page =
      typeof pageRaw === "number"
        ? pageRaw
        : typeof pageRaw === "string" && /^\d+$/.test(pageRaw)
          ? Number(pageRaw)
          : undefined;
    if (page !== undefined) out["page"] = page;
    if (typeof raw["orderby"] === "string") out["orderby"] = raw["orderby"];
    const ascRaw = raw["asc"];
    const asc =
      typeof ascRaw === "boolean"
        ? ascRaw
        : ascRaw === "true"
          ? true
          : ascRaw === "false"
            ? false
            : undefined;
    if (asc !== undefined) out["asc"] = asc;
    if (typeof raw["search"] === "string") out["search"] = raw["search"];
    if (typeof raw["aggSearch"] === "string") out["aggSearch"] = raw["aggSearch"];
    if (typeof raw["rejSearch"] === "string") out["rejSearch"] = raw["rejSearch"];
    return out as RulesSearchParams;
  },
});

// Dev-only showroom routes (component gallery + defineResource demo). Built
// only under Vite's DEV flag: in production `import.meta.env.DEV` folds to
// false, so Rollup drops these routes AND the PrimitivesPage/ResourcePage
// modules from the bundle.
const devRoutes = import.meta.env.DEV
  ? [
      createRoute({
        getParentRoute: () => webLayoutRoute,
        path: "/web/dev/primitives",
        component: PrimitivesPage,
      }),
      createRoute({
        getParentRoute: () => webLayoutRoute,
        path: "/web/dev/resource",
        component: ResourcePage,
      }),
    ]
  : [];

type HeartbeatsSearchParams = {
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
  search?: string;
  status?: string;
};

const heartbeatsRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/heartbeats",
  component: lazyRouteComponent(
    () => import("@/features/heartbeats/HeartbeatsPage"),
    "HeartbeatsPage",
  ),
  validateSearch: (raw): HeartbeatsSearchParams => {
    const out: Record<string, unknown> = {};
    if (typeof raw["uid"] === "string") out["uid"] = raw["uid"];
    const pageRaw = raw["page"];
    const page =
      typeof pageRaw === "number"
        ? pageRaw
        : typeof pageRaw === "string" && /^\d+$/.test(pageRaw)
          ? Number(pageRaw)
          : undefined;
    if (page !== undefined) out["page"] = page;
    if (typeof raw["orderby"] === "string") out["orderby"] = raw["orderby"];
    const ascRaw = raw["asc"];
    const asc =
      typeof ascRaw === "boolean"
        ? ascRaw
        : ascRaw === "true"
          ? true
          : ascRaw === "false"
            ? false
            : undefined;
    if (asc !== undefined) out["asc"] = asc;
    if (typeof raw["search"] === "string") out["search"] = raw["search"];
    if (typeof raw["status"] === "string") out["status"] = raw["status"];
    return out as HeartbeatsSearchParams;
  },
});

type AuthAuditSearchParams = {
  page?: number;
  orderby?: string;
  asc?: boolean;
  search?: string;
};

const authAuditRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/admin/audit",
  component: lazyRouteComponent(
    () => import("@/features/admin/audit/AuthAuditPage"),
    "AuthAuditPage",
  ),
  validateSearch: (raw): AuthAuditSearchParams => {
    const out: Record<string, unknown> = {};
    const pageRaw = raw["page"];
    const page =
      typeof pageRaw === "number"
        ? pageRaw
        : typeof pageRaw === "string" && /^\d+$/.test(pageRaw)
          ? Number(pageRaw)
          : undefined;
    if (page !== undefined) out["page"] = page;
    if (typeof raw["orderby"] === "string") out["orderby"] = raw["orderby"];
    const ascRaw = raw["asc"];
    const asc =
      typeof ascRaw === "boolean"
        ? ascRaw
        : ascRaw === "true"
          ? true
          : ascRaw === "false"
            ? false
            : undefined;
    if (asc !== undefined) out["asc"] = asc;
    if (typeof raw["search"] === "string") out["search"] = raw["search"];
    return out as AuthAuditSearchParams;
  },
});

// Catch-all for any other `/web/*` path (a stale bookmark, a typo, a link
// into a feature that moved). A literal sibling route always wins over this
// splat, so it only ever catches genuine dead ends — rendered as a real page
// (not a thrown not-found) so it's not at the mercy of the router's fuzzy
// not-found resolution picking some other ancestor's notFoundComponent.
const webCatchAllRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/$",
  component: NotFound,
});

const routeTree = rootRoute.addChildren([
  indexRoute,
  loginRoute,
  loginCallbackRoute,
  webLayoutRoute.addChildren([
    webIndexRoute,
    alertsRoute,
    rulesRoute,
    snoozesRoute,
    notificationsRoute,
    heartbeatsRoute,
    dashboardRoute,
    usersRoute,
    rolesRoute,
    groupsRoute,
    apikeysRoute,
    environmentsRoute,
    widgetsRoute,
    kvRoute,
    settingsRoute,
    inputsRoute,
    statusRoute,
    tenantsRoute,
    tenantRoutingRoute,
    authAuditRoute,
    ...devRoutes,
    profileRoute,
    webCatchAllRoute,
  ]),
]);

export const router = createRouter({
  routeTree,
  // Prefetch a route's lazy chunk on link hover/focus so the chunk is usually
  // resident by the time the user clicks. The pending component shows only
  // when a click outruns the in-flight fetch past defaultPendingMs (so fast
  // chunk loads never flash it) — kept minimal: a centered Spinner over the
  // route outlet area.
  defaultPreload: "intent",
  defaultPendingComponent: () => (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        minHeight: "40vh",
      }}
    >
      <Spinner size={20} label="Loading page" />
    </div>
  ),
});

setUnauthorizedHandler(() => {
  // The server just refused this session, so its refresh token is already
  // dead — skip the revoke round trip.
  authStore.getState().logout({ revoke: false });
  // Remember where the operator was so signing back in returns them to it,
  // rather than dropping them on their default landing page. Never capture the
  // login page itself as a destination (that would loop).
  const here = `${window.location.pathname}${window.location.search}`;
  void router.navigate({ to: "/web/login", search: loginRedirectSearch(here) });
});

// Keep the access token fresh in the background for as long as the tab lives.
startSessionRefresh();

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
