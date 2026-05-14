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
import { PrimitivesPage } from "@/features/dev/PrimitivesPage";
import type { IconName } from "@/shared/icons/icon-names";

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
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  beforeLoad: () => {
    // eslint-disable-next-line @typescript-eslint/only-throw-error
    throw redirect({ to: "/web/alerts" as string });
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
  { path: "/web/alerts", title: "Alerts", icon: "file-text", m: "M3" },
  { path: "/web/dashboard", title: "Dashboard", icon: "gauge", m: "M6" },
  { path: "/web/snoozes", title: "Snoozes", icon: "bell-off", m: "M5" },
  { path: "/web/rules", title: "Rules", icon: "scale", m: "M4" },
  { path: "/web/notifications", title: "Notifications", icon: "megaphone", m: "M5" },
  { path: "/web/admin/users", title: "Users", icon: "users", m: "M7" },
  { path: "/web/admin/roles", title: "Roles", icon: "user-plus", m: "M7" },
  { path: "/web/admin/environments", title: "Environments", icon: "layers", m: "M7" },
  { path: "/web/admin/widgets", title: "Widgets", icon: "plug", m: "M7" },
  { path: "/web/admin/kv", title: "Key-values", icon: "book", m: "M7" },
  { path: "/web/admin/settings", title: "Settings", icon: "settings", m: "M7" },
  { path: "/web/admin/status", title: "Status", icon: "activity", m: "M7" },
  { path: "/web/profile", title: "Profile", icon: "sliders", m: "M2" },
];

const featureRoutes = features.map((cfg) =>
  createRoute({
    getParentRoute: () => webLayoutRoute,
    path: cfg.path,
    component: placeholder(cfg.title, cfg.icon, cfg.m),
  }),
);

const primitivesRoute = createRoute({
  getParentRoute: () => webLayoutRoute,
  path: "/web/dev/primitives",
  component: PrimitivesPage,
});

const routeTree = rootRoute.addChildren([
  indexRoute,
  webLayoutRoute.addChildren([webIndexRoute, ...featureRoutes, primitivesRoute]),
]);

export const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
