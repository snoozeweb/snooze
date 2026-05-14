import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
  createMemoryHistory,
  Outlet,
} from "@tanstack/react-router";
import { AppShell } from "./AppShell";

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
  return render(<RouterProvider router={router} />);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-assignment */
}

describe("AppShell", () => {
  it("renders the Topbar, Sidebar, and the matched route's content", () => {
    setup("/web/alerts");
    expect(screen.getByText("Snooze")).toBeInTheDocument();
    expect(screen.getByText("Alerts page")).toBeInTheDocument();
  });

  it("shows the current page name in the breadcrumb", () => {
    setup("/web/dashboard");
    // Dashboard appears in sidebar AND breadcrumb — assert at least 2 instances
    expect(screen.getAllByText("Dashboard").length).toBeGreaterThanOrEqual(2);
  });
});
