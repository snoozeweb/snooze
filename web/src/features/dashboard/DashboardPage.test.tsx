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
import { alertsSearchForBucket } from "./bucket-utils";

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

const FULL_STATS_RESPONSE = {
  data: {
    series: [
      { t: "2026-05-14T00:00:00Z", counts: { Alerts: 10, Throttled: 2, Snoozed: 1 } },
      { t: "2026-05-14T01:00:00Z", counts: { Alerts: 5, Throttled: 1, "Action error": 1 } },
    ],
    totals: {
      by_severity: { critical: 4, warning: 8, info: 3 },
      by_environment: { prod: 10, staging: 5 },
      by_host: { "host-a": 7, "host-b": 4, "host-c": 2 },
      by_action_success: { slack: 6, email: 3 },
      by_action_failure: { pagerduty: 1 },
      by_throttled: { rule1: 2, rule2: 1 },
      by_snoozed: { filter1: 3 },
      by_notification: { slack: 5 },
    },
    snapshot: {
      by_state: { open: 5, ack: 3, closed: 7 },
      total_hits: 15,
      open: 5,
      ack: 3,
      closed: 7,
    },
    weekday: { "0": 3, "1": 8, "2": 9, "3": 7, "4": 10, "5": 5, "6": 2 },
  },
  meta: { from: "2026-05-13T00:00:00Z", to: "2026-05-14T00:00:00Z", bucket: 3600 },
};

const COMMENTS_RESPONSE = {
  data: [
    {
      uid: "c1",
      record_uid: "alert-abc",
      type: "ack",
      user: "alice",
      message: "looks good",
      date_epoch: Math.floor(Date.now() / 1000),
    },
    {
      uid: "c2",
      record_uid: "alert-xyz",
      type: "close",
      user: "bob",
      message: null,
      date_epoch: Math.floor(Date.now() / 1000) - 60,
    },
  ],
  meta: { count: 2, limit: 15, offset: 0, total: 2 },
};

function setup() {
  const root = createRootRoute({ component: () => <Outlet /> });
  // Add /web/alerts route so ActivityFeed's <Link to="/web/alerts"> resolves.
  const alertsRoute = createRoute({
    getParentRoute: () => root,
    path: "/web/alerts",
    component: () => <div>alerts</div>,
    validateSearch: (raw): { search?: string } => {
      if (typeof raw["search"] === "string") return { search: raw["search"] };
      return {};
    },
  });
  const route = createRoute({
    getParentRoute: () => root,
    path: "/web/dashboard",
    component: DashboardPage,
  });
  const tree = root.addChildren([alertsRoute, route]);
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
              by_host: {},
              by_action_success: {},
              by_action_failure: {},
              by_throttled: {},
              by_snoozed: {},
              by_notification: {},
            },
            snapshot: { by_state: {}, total_hits: 0, open: 0, ack: 0, closed: 0 },
            weekday: {},
          },
          meta: { from: "", to: "", bucket: 3600 },
        }),
      ),
      http.get("/api/v1/comment", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 15, offset: 0, total: 0 } }),
      ),
    );
    setup();
    expect(screen.getByRole("heading", { level: 1, name: /dashboard/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "1d" })).toBeInTheDocument();
  });

  it("renders all cockpit panels when stats data is non-empty", async () => {
    mswServer.use(
      http.get("/api/v1/stats", () => HttpResponse.json(FULL_STATS_RESPONSE)),
      http.get("/api/v1/comment", () => HttpResponse.json(COMMENTS_RESPONSE)),
    );
    setup();

    expect(await screen.findByText("Alerts over time")).toBeInTheDocument();
    expect(screen.getByText("Recent activity")).toBeInTheDocument();
    expect(screen.getByText("By severity")).toBeInTheDocument();
    expect(screen.getByText("By environment")).toBeInTheDocument();
    expect(screen.getByText("By state")).toBeInTheDocument();
    expect(screen.getByText("Top hosts")).toBeInTheDocument();
    expect(screen.getByText("Actions")).toBeInTheDocument();
    expect(screen.getByText("Throttled by rule")).toBeInTheDocument();
    expect(screen.getByText("Snoozed by filter")).toBeInTheDocument();
    expect(screen.getByText("By weekday")).toBeInTheDocument();
  });

  it("renders charts when stats data is non-empty", async () => {
    mswServer.use(
      http.get("/api/v1/stats", () => HttpResponse.json(FULL_STATS_RESPONSE)),
      http.get("/api/v1/comment", () => HttpResponse.json(COMMENTS_RESPONSE)),
    );
    const { container } = setup();
    await waitFor(() => expect(container.querySelectorAll("canvas").length).toBeGreaterThan(0));
  });
});

describe("alertsSearchForBucket", () => {
  it("returns a date_epoch range string for a given ISO bucket start and bucket size", () => {
    // 2026-05-14T00:00:00Z = 1747180800 (Unix)
    const x = "2026-05-14T00:00:00Z";
    const bucket = 3600;
    const from = Math.floor(Date.parse(x) / 1000);
    const to = from + bucket;
    expect(alertsSearchForBucket(x, bucket)).toBe(
      `date_epoch > ${from} and date_epoch < ${to}`,
    );
  });

  it("handles a 6-hour bucket correctly", () => {
    const x = "2026-05-14T12:00:00Z";
    const bucket = 21600;
    const from = Math.floor(Date.parse(x) / 1000);
    const to = from + bucket;
    expect(alertsSearchForBucket(x, bucket)).toBe(
      `date_epoch > ${from} and date_epoch < ${to}`,
    );
  });
});
