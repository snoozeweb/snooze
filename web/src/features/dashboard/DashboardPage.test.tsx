import { render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { DashboardPage } from "./DashboardPage";

// Chart.js canvas stub for jsdom.
beforeAll(() => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-member-access
  (HTMLCanvasElement.prototype as any).getContext = () => ({
    save: () => undefined,
    restore: () => undefined,
    fillRect: () => undefined,
    clearRect: () => undefined,
    measureText: () => ({ width: 0 }),
    beginPath: () => undefined,
    closePath: () => undefined,
    stroke: () => undefined,
    fill: () => undefined,
    moveTo: () => undefined,
    lineTo: () => undefined,
    arc: () => undefined,
    translate: () => undefined,
    setTransform: () => undefined,
    transform: () => undefined,
    rotate: () => undefined,
    scale: () => undefined,
    rect: () => undefined,
    fillText: () => undefined,
    strokeText: () => undefined,
    canvas: { width: 100, height: 100 },
  });
});

function setup() {
  const root = createRootRoute({ component: () => <Outlet /> });
  const route = createRoute({
    getParentRoute: () => root,
    path: "/web/dashboard",
    component: DashboardPage,
  });
  const tree = root.addChildren([route]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: ["/web/dashboard"] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <ToastProvider>
          {/* router is locally constructed; cast needed for the registered-router type mismatch */}
          <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
          <Toaster />
        </ToastProvider>
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("DashboardPage", () => {
  it("renders the title and the time-range picker", () => {
    mswServer.use(
      http.get("/api/v1/stats", () =>
        HttpResponse.json({
          data: {
            series: [],
            totals: {
              by_severity: {},
              by_environment: {},
              by_action_success: {},
              by_action_failure: {},
            },
          },
          meta: { from: "", to: "", bucket: 3600 },
        }),
      ),
    );
    setup();
    expect(screen.getByRole("heading", { level: 1, name: /dashboard/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "1d" })).toBeInTheDocument();
  });

  it("renders a chart when stats data is non-empty", async () => {
    mswServer.use(
      http.get("/api/v1/stats", () =>
        HttpResponse.json({
          data: {
            series: [{ t: "2026-05-14T00:00:00Z", counts: { syslog: 5 } }],
            totals: {
              by_severity: { info: 5 },
              by_environment: { prod: 5 },
              by_action_success: { slack: 2 },
              by_action_failure: {},
            },
          },
          meta: { from: "", to: "", bucket: 3600 },
        }),
      ),
    );
    const { container } = setup();
    await waitFor(() => expect(container.querySelectorAll("canvas").length).toBeGreaterThan(0));
  });
});
