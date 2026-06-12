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
import { Spinner } from "@/shared/ui/Spinner";
import { AppShell } from "./layout/AppShell";
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
          <Outlet />
          <Toaster />
        </ToastProvider>
      </TooltipProvider>
    </QueryClientProvider>
  ),
});

const webLayoutRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "weblayout",
  component: AppShell,
  beforeLoad: ({ location }) => {
    if (!authStore.getState().isAuthenticated) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error
      throw redirect({
        to: "/web/login",
        search: {
          return_to: encodeURIComponent(location.href),
        },
      });
    }
  },
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  beforeLoad: () => {
    // eslint-disable-next-line @typescript-eslint/only-throw-error
    throw redirect({ to: "/web/alerts" as string });
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
    throw redirect({ to: "/web/alerts" as string });
  },
});

type UsersSearchParams = {
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
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
    return out as UsersSearchParams;
  },
});

type RolesSearchParams = {
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
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
    return out as RolesSearchParams;
  },
});

type EnvironmentsSearchParams = {
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
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
    return out as EnvironmentsSearchParams;
  },
});

type WidgetsSearchParams = {
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
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
    return out as WidgetsSearchParams;
  },
});

type KVSearchParams = {
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
  dict?: string;
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
    return out as KVSearchParams;
  },
});

type SettingsSearchParams = {
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

type AlertsSearchParams = {
  state?: string;
  severity?: string;
  environment?: string;
  search?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
  uid?: string;
  // Lifecycle tab id and comma-separated environment UIDs. AlertsPage drives
  // its filter state off these (and the dashboard drill-downs deep-link to
  // them), so validateSearch must preserve them — otherwise they'd be
  // stripped on every navigation through this route.
  tab?: string;
  env?: string;
};

const alertsRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/alerts",
  component: lazyRouteComponent(() => import("@/features/alerts/AlertsPage"), "AlertsPage"),
  validateSearch: (raw): AlertsSearchParams => {
    const out: Record<string, unknown> = {};
    const s = (k: string) => (typeof raw[k] === "string" ? raw[k] : undefined);
    const n = (k: string) => {
      const v = raw[k];
      if (typeof v === "number") return v;
      if (typeof v === "string" && /^\d+$/.test(v)) return Number(v);
      return undefined;
    };
    const b = (k: string) => {
      const v = raw[k];
      if (typeof v === "boolean") return v;
      if (v === "true") return true;
      if (v === "false") return false;
      return undefined;
    };
    const setIf = (k: string, v: unknown) => {
      if (v !== undefined) out[k] = v;
    };
    setIf("state", s("state"));
    setIf("severity", s("severity"));
    setIf("environment", s("environment"));
    setIf("search", s("search"));
    setIf("page", n("page"));
    setIf("orderby", s("orderby"));
    setIf("asc", b("asc"));
    setIf("uid", s("uid"));
    setIf("tab", s("tab"));
    setIf("env", s("env"));
    return out as AlertsSearchParams;
  },
});

type SnoozesSearchParams = {
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
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
    return out as SnoozesSearchParams;
  },
});

type NotificationsSearchParams = {
  tab?: "notifications" | "actions";
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
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
    return out as NotificationsSearchParams;
  },
});

const dashboardRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/dashboard",
  component: lazyRouteComponent(
    () => import("@/features/dashboard/DashboardPage"),
    "DashboardPage",
  ),
});

const profileRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/profile",
  component: lazyRouteComponent(() => import("@/features/auth/Profile"), "Profile"),
});

type RulesSearchParams = {
  tab?: "rules" | "aggregates";
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
};

const rulesRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/rules",
  component: lazyRouteComponent(() => import("@/features/rules/RulesPage"), "RulesPage"),
  validateSearch: (raw): RulesSearchParams => {
    const out: Record<string, unknown> = {};
    const tab = typeof raw["tab"] === "string" ? raw["tab"] : undefined;
    if (tab === "rules" || tab === "aggregates") out["tab"] = tab;
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
    dashboardRoute,
    usersRoute,
    rolesRoute,
    environmentsRoute,
    widgetsRoute,
    kvRoute,
    settingsRoute,
    statusRoute,
    tenantsRoute,
    ...devRoutes,
    profileRoute,
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
  authStore.getState().logout();
  void router.navigate({ to: "/web/login" });
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
