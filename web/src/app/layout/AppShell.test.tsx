import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
  createMemoryHistory,
  Outlet,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { authStore } from "@/lib/auth/store";
import { AppShell } from "./AppShell";

function loginWithPerms(perms: string[]) {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({ sub: "x", exp: Math.floor(Date.now() / 1000) + 3600, permissions: perms }),
  );
  authStore.getState().login(`${header}.${body}.sig`);
}

function setup(pathname = "/web/alerts") {
  const root = createRootRoute({ component: () => <Outlet /> });
  const shell = createRoute({ getParentRoute: () => root, id: "shell", component: AppShell });
  const alerts = createRoute({
    getParentRoute: () => shell,
    path: "/web/alerts",
    component: () => <p>Alerts page</p>,
  });
  const dashboard = createRoute({
    getParentRoute: () => shell,
    path: "/web/dashboard",
    component: () => <p>Dashboard page</p>,
  });
  const tree = root.addChildren([shell.addChildren([alerts, dashboard])]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-assignment */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: [pathname] }),
  }) as any;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-assignment */
}

function mockMatchMedia(matches: boolean) {
  vi.stubGlobal(
    "matchMedia",
    vi.fn().mockImplementation((query: string) => ({
      matches,
      media: query,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    })),
  );
}

describe("AppShell", () => {
  afterEach(() => {
    localStorage.clear();
    authStore.getState().logout();
    vi.unstubAllGlobals();
  });

  it("renders the Topbar, Sidebar, and the matched route's content", () => {
    // jsdom has no matchMedia → useIsMobileShell defaults to desktop (false).
    setup("/web/alerts");
    expect(screen.getByRole("img", { name: /snooze/i })).toBeInTheDocument();
    expect(screen.getByText("Alerts page")).toBeInTheDocument();
  });

  it("shows the current page name in the breadcrumb", () => {
    // Log in with dashboard permission so the sidebar also shows Dashboard
    loginWithPerms(["ro_stats"]);
    setup("/web/dashboard");
    // Dashboard appears in sidebar AND breadcrumb — assert at least 2 instances
    expect(screen.getAllByText("Dashboard").length).toBeGreaterThanOrEqual(2);
  });

  it("renders the desktop Sidebar and no BottomNav above the shell breakpoint", () => {
    mockMatchMedia(false);
    loginWithPerms(["ro_record"]);
    setup("/web/alerts");
    // The desktop sidebar is the <aside> landmark labeled "Primary navigation".
    expect(screen.getByRole("complementary", { name: /primary navigation/i })).toBeInTheDocument();
    // The bottom bar is the <nav> labeled "Primary" — absent on desktop.
    expect(screen.queryByRole("navigation", { name: /^primary$/i })).toBeNull();
  });

  it("renders the BottomNav and hides the Sidebar below the shell breakpoint", () => {
    mockMatchMedia(true);
    loginWithPerms(["ro_record"]);
    setup("/web/alerts");
    // The bottom-tab bar (<nav aria-label="Primary">) is present…
    expect(screen.getByRole("navigation", { name: /^primary$/i })).toBeInTheDocument();
    // …and the desktop sidebar (<aside aria-label="Primary navigation">) is not.
    expect(screen.queryByRole("complementary", { name: /primary navigation/i })).toBeNull();
  });
});
