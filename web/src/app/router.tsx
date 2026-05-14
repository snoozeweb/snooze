import {
  createRoute,
  createRootRoute,
  createRouter,
  Outlet,
  redirect,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { AppShell } from "./layout/AppShell";
import { PlaceholderPage } from "./PlaceholderPage";
import { AlertsPage } from "@/features/alerts/AlertsPage";
import { RulesPage } from "@/features/rules/RulesPage";
import { SnoozesPage } from "@/features/snoozes/SnoozesPage";
import { NotificationsPage } from "@/features/notifications/NotificationsPage";
import { DashboardPage } from "@/features/dashboard/DashboardPage";
import { PrimitivesPage } from "@/features/dev/PrimitivesPage";
import { ResourcePage } from "@/features/dev/ResourcePage";
import type { IconName } from "@/shared/icons/icon-names";
import { authStore } from "@/lib/auth/store";
import { Login } from "@/features/auth/Login";
import { Profile } from "@/features/auth/Profile";
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
  validateSearch: (search): { return_to?: string } => {
    if (typeof search["return_to"] === "string") {
      return { return_to: search["return_to"] };
    }
    return {};
  },
});

const webIndexRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/",
  beforeLoad: () => {
    // eslint-disable-next-line @typescript-eslint/only-throw-error
    throw redirect({ to: "/web/alerts" as string });
  },
});

function placeholder(title: string, icon: IconName, milestone: string) {
  return function PlaceholderRoute() {
    return <PlaceholderPage title={title} icon={icon} milestone={milestone} />;
  };
}

const features: ReadonlyArray<{ path: string; title: string; icon: IconName; m: string }> = [
  { path: "/web/admin/users", title: "Users", icon: "users", m: "M7" },
  { path: "/web/admin/roles", title: "Roles", icon: "user-plus", m: "M7" },
  { path: "/web/admin/environments", title: "Environments", icon: "layers", m: "M7" },
  { path: "/web/admin/widgets", title: "Widgets", icon: "plug", m: "M7" },
  { path: "/web/admin/kv", title: "Key-values", icon: "book", m: "M7" },
  { path: "/web/admin/settings", title: "Settings", icon: "settings", m: "M7" },
  { path: "/web/admin/status", title: "Status", icon: "activity", m: "M7" },
];

const featureRoutes = features.map((cfg) =>
  createRoute({
    getParentRoute: () => webLayoutRoute,
    path: cfg.path,
    component: placeholder(cfg.title, cfg.icon, cfg.m),
  }),
);

type AlertsSearchParams = {
  state?: string;
  severity?: string;
  environment?: string;
  search?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
  uid?: string;
};

const alertsRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/alerts",
  component: AlertsPage,
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
  component: SnoozesPage,
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
  component: NotificationsPage,
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
  component: DashboardPage,
});

const primitivesRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/dev/primitives",
  component: PrimitivesPage,
});

const resourcePageRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/dev/resource",
  component: ResourcePage,
});

const profileRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/profile",
  component: Profile,
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
  component: RulesPage,
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

const routeTree = rootRoute.addChildren([
  indexRoute,
  loginRoute,
  webLayoutRoute.addChildren([
    webIndexRoute,
    alertsRoute,
    rulesRoute,
    snoozesRoute,
    notificationsRoute,
    dashboardRoute,
    ...featureRoutes,
    primitivesRoute,
    resourcePageRoute,
    profileRoute,
  ]),
]);

export const router = createRouter({ routeTree });

setUnauthorizedHandler(() => {
  authStore.getState().logout();
  void router.navigate({ to: "/web/login" });
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
