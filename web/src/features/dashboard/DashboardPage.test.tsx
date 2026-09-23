import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
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
import { authStore } from "@/lib/auth/store";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { LiveAnnouncerProvider } from "@/shared/a11y/LiveAnnouncer";
import { DashboardPage } from "./DashboardPage";
import { alertsSearchForBucket, alertsSearchForRange } from "./bucket-utils";

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
      // by_state keys are the raw record `state` wire values (see
      // internal/pluginimpl/stats/plugin.go) — "close", not "closed".
      by_state: { open: 5, ack: 3, close: 7 },
      total_hits: 15,
      open: 5,
      ack: 3,
      closed: 7,
    },
    weekday: { "0": 3, "1": 8, "2": 9, "3": 7, "4": 10, "5": 5, "6": 2 },
  },
  meta: {
    from: "2026-05-13T00:00:00Z",
    to: "2026-05-14T00:00:00Z",
    bucket: 3600,
    counters: { enabled: true, present: true },
  },
};

const EMPTY_STATS_DATA = {
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

// Mirror the real alerts route's search allowlist so drill-down deep-links
// (?search=, ?tab=, ?env=) round-trip in these tests just as they do live.
function alertsValidateSearch(raw: Record<string, unknown>): {
  search?: string;
  tab?: string;
  env?: string;
} {
  const out: { search?: string; tab?: string; env?: string } = {};
  if (typeof raw["search"] === "string") out.search = raw["search"];
  if (typeof raw["tab"] === "string") out.tab = raw["tab"];
  if (typeof raw["env"] === "string") out.env = raw["env"];
  return out;
}

function setup(initialEntry = "/web/dashboard") {
  const root = createRootRoute({ component: () => <Outlet /> });
  // Add /web/alerts route so ActivityFeed's <Link to="/web/alerts"> resolves
  // and so drill-down navigations land somewhere with the search preserved.
  const alertsRoute = createRoute({
    getParentRoute: () => root,
    path: "/web/alerts",
    component: () => <div>alerts</div>,
    validateSearch: alertsValidateSearch,
  });
  // Add /web/notifications so the NotificationsPanel deep link resolves.
  const notificationsRoute = createRoute({
    getParentRoute: () => root,
    path: "/web/notifications",
    component: () => <div>notifications</div>,
    validateSearch: (raw: Record<string, unknown>) => raw,
  });
  const route = createRoute({
    getParentRoute: () => root,
    path: "/web/dashboard",
    component: DashboardPage,
    // Mirrors the real route's allowlist (router.tsx) — the view AND the time
    // window. A mirror that dropped `range`/`from`/`to` could not catch a view
    // switch clobbering the window the operator picked, which is the one way
    // these two URL-backed controls can break each other.
    validateSearch: (raw: Record<string, unknown>) => {
      const out: Record<string, unknown> = {};
      if (raw["view"] === "analyses") out["view"] = "analyses";
      const range = raw["range"];
      if (
        range === "1d" ||
        range === "1w" ||
        range === "1m" ||
        range === "1y" ||
        range === "custom"
      )
        out["range"] = range;
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
      return out;
    },
  });
  const tree = root.addChildren([alertsRoute, notificationsRoute, route]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const utils = render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <ToastProvider>
          {/* Mirrors the real tree (router.tsx mounts this at the root) so the
              page's refresh announcements land in a live region here too. */}
          <LiveAnnouncerProvider>
            {/* router is locally constructed; cast needed for the registered-router type mismatch */}
            <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
            <Toaster />
          </LiveAnnouncerProvider>
        </ToastProvider>
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { ...utils, router, client };
}

// Two environments so the "By environment" drill-down can resolve name→uid.
const ENV_RESPONSE = {
  data: [
    { uid: "env-prod", name: "prod", tree_order: 0 },
    { uid: "env-staging", name: "staging", tree_order: 1 },
  ],
  meta: { count: 2, limit: 200, offset: 0, total: 2 },
};

/** The condition a /record request carried, as readable JSON. */
function decodeQ(url: string): string {
  const q = new URL(url).searchParams.get("q") ?? "";
  if (q === "") return "";
  const b64 = q.replace(/-/g, "+").replace(/_/g, "/");
  return atob(b64 + "=".repeat((4 - (b64.length % 4)) % 4));
}

function mockFullDashboard() {
  mswServer.use(
    http.get("/api/v1/stats", () => HttpResponse.json(FULL_STATS_RESPONSE)),
    http.get("/api/v1/comment", () => HttpResponse.json(COMMENTS_RESPONSE)),
    http.get("/api/v1/environment", () => HttpResponse.json(ENV_RESPONSE)),
  );
}

function loginWithPerms(perms: string[]) {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({
      sub: "tester",
      exp: Math.floor(Date.now() / 1000) + 3600,
      permissions: perms,
    }),
  );
  authStore.getState().login(`${header}.${body}.sig`);
}

describe("DashboardPage", () => {
  // The Notifications panel only links into the inspector's Deliveries tab for
  // a role that can read the delivery log.
  beforeEach(() =>
    loginWithPerms(["ro_stats", "ro_record", "ro_notification", "ro_notificationlog"]),
  );
  afterEach(() => authStore.getState().logout());

  it("renders the title and the time-range picker", () => {
    mswServer.use(
      http.get("/api/v1/stats", () =>
        HttpResponse.json({
          data: EMPTY_STATS_DATA,
          meta: { from: "", to: "", bucket: 3600, counters: { enabled: true, present: true } },
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

  it("leads with noise removed, then the stream, then supporting panels", async () => {
    mswServer.use(
      http.get("/api/v1/stats", () => HttpResponse.json(FULL_STATS_RESPONSE)),
      http.get("/api/v1/comment", () => HttpResponse.json(COMMENTS_RESPONSE)),
    );
    setup();

    // The thesis panel, with both suppression breakdowns folded into it.
    expect(await screen.findByText("Noise removed")).toBeInTheDocument();
    expect(screen.getByText("Throttled by rule")).toBeInTheDocument();
    expect(screen.getByText("Snoozed by filter")).toBeInTheDocument();

    expect(screen.getByText("Alerts over time")).toBeInTheDocument();
    expect(screen.getByText("By state")).toBeInTheDocument();
    expect(screen.getByText("Recent activity")).toBeInTheDocument();

    // The secondary breakdowns live behind one tabbed panel instead of four
    // co-equal cards.
    expect(screen.getByText("Breakdowns")).toBeInTheDocument();
    for (const tab of ["Severity", "Environment", "Hosts", "Actions", "Notifications", "Weekday"]) {
      expect(screen.getByRole("tab", { name: tab })).toBeInTheDocument();
    }
    // The analyses list is a page-level view now, not the seventh tab of a
    // card in the bottom-right corner.
    expect(screen.queryByRole("tab", { name: "Analyses" })).not.toBeInTheDocument();
  });

  it("lists notifications by send count on the Notifications tab, linking a resolved name", async () => {
    mswServer.use(
      http.get("/api/v1/stats", () => HttpResponse.json(FULL_STATS_RESPONSE)),
      http.get("/api/v1/comment", () => HttpResponse.json(COMMENTS_RESPONSE)),
      http.get("/api/v1/notification", () =>
        HttpResponse.json({
          data: [{ uid: "nt-slack", name: "slack" }],
          meta: { count: 1, limit: 500, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await screen.findByText("Noise removed");

    await user.click(screen.getByRole("tab", { name: "Notifications" }));
    const link = await screen.findByRole("link", { name: "slack" });
    const href = link.getAttribute("href") ?? "";
    expect(href).toContain("/web/notifications");
    expect(href).toContain("details=nt-slack");
  });

  // The Analyses view is live, not windowed: it lists the open alerts an
  // analysis has been written onto. It is reached from the title row's view
  // switch or from the Right-now tile, and it states its own count against the
  // page's existing ACTIVE_ALERTS probe rather than asking for a second total.
  describe("the Analyses view", () => {
    function mockAnalyses() {
      mswServer.use(
        http.get("/api/v1/stats", () => HttpResponse.json(FULL_STATS_RESPONSE)),
        http.get("/api/v1/comment", () => HttpResponse.json(COMMENTS_RESPONSE)),
        http.get("/api/v1/record", ({ request }) => {
          const cond = decodeQ(request.url);
          // Three probes ask for one row and read meta.total: the
          // needs-attention queue (ACTIVE_ALERTS, the only one with an `ack`
          // clause), the open population behind the Analysed tile's
          // denominator, and the analysed count itself. Only the last two
          // carry the `agentic` clause.
          if (new URL(request.url).searchParams.get("limit") === "1") {
            return HttpResponse.json({
              data: [],
              meta: {
                count: 0,
                limit: 1,
                offset: 0,
                total: cond.includes('"agentic"') ? 1 : 37,
              },
            });
          }
          return HttpResponse.json({
            data: [
              {
                uid: "r-disk",
                host: "srv-victoria1",
                severity: "critical",
                state: "open",
                date_epoch: 1_700_000_000,
                agentic: {
                  root_cause: { summary: "Orphaned blobs filled /var", confidence: "high" },
                  remediation_plan: { steps: [{ action: "prune", risk: "low" }] },
                  analysis: { at: "2026-09-20T10:00:00Z", by: "agent-bot", source: "alert-rca" },
                },
              },
            ],
            meta: { count: 1, limit: 50, offset: 0, total: 1 },
          });
        }),
      );
    }

    it("switches to the analysed alerts from the title row, and hides the time picker there", async () => {
      mockAnalyses();
      const user = userEvent.setup();
      const { router } = setup();
      await screen.findByText("Noise removed");

      const viewSwitch = screen.getByRole("group", { name: "Dashboard view" });
      await user.click(within(viewSwitch).getByRole("button", { name: "Analyses" }));

      expect(await screen.findByText("Orphaned blobs filled /var")).toBeInTheDocument();
      // The ratio against the open backlog lives on the Right-now tile, not
      // above this list: the view lists every analysed alert and says nothing
      // about the ones it does not cover.
      expect(screen.queryByRole("link", { name: /\d+ open$/ })).not.toBeInTheDocument();
      // The view is "right now", so the window picker has no say in it.
      expect(screen.queryByRole("button", { name: "1d" })).not.toBeInTheDocument();
      // Shareable: the view rides in the URL.
      expect(router.state.location.search).toMatchObject({ view: "analyses" });
    });

    it("opens straight onto the view from a deep link", async () => {
      mockAnalyses();
      setup("/web/dashboard?view=analyses");

      expect(await screen.findByText("Orphaned blobs filled /var")).toBeInTheDocument();
      expect(screen.queryByText("Noise removed")).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "1d" })).not.toBeInTheDocument();
    });

    it("keeps the picked window across a view switch", async () => {
      mockAnalyses();
      const user = userEvent.setup();
      const { router } = setup("/web/dashboard?range=custom&from=1757000000000&to=1757600000000");
      await screen.findByText("Noise removed");

      const viewSwitch = screen.getByRole("group", { name: "Dashboard view" });
      await user.click(within(viewSwitch).getByRole("button", { name: "Analyses" }));
      expect(router.state.location.search).toMatchObject({
        view: "analyses",
        range: "custom",
        from: 1757000000000,
        to: 1757600000000,
      });

      await user.click(within(viewSwitch).getByRole("button", { name: "Overview" }));
      // The window survives the round trip, and the default view drops its
      // param rather than spelling itself out.
      expect(router.state.location.search).toMatchObject({
        range: "custom",
        from: 1757000000000,
        to: 1757600000000,
      });
      expect(router.state.location.search).not.toHaveProperty("view");
    });

    it("keeps Back on the dashboard when a deep-linked view is switched away", async () => {
      mockAnalyses();
      const user = userEvent.setup();
      const { router } = setup("/web/dashboard?view=analyses");
      await screen.findByText("Orphaned blobs filled /var");

      const viewSwitch = screen.getByRole("group", { name: "Dashboard view" });
      await user.click(within(viewSwitch).getByRole("button", { name: "Overview" }));
      await screen.findByText("Noise removed");

      // Arriving on ?view=analyses makes the switch a step worth keeping: with
      // `replace`, Back walks out of the dashboard entirely.
      act(() => router.history.back());
      await waitFor(() => expect(router.state.location.search).toMatchObject({ view: "analyses" }));
    });

    it("counts the analysed alerts on a Right-now tile that opens the view", async () => {
      mockAnalyses();
      const user = userEvent.setup();
      const { router } = setup();

      const live = await screen.findByRole("region", { name: "Right now" });
      const tile = within(live).getByText("Analysed");
      expect(within(live).getByText("of 37 open")).toBeInTheDocument();

      await user.click(tile);
      expect(router.state.location.search).toMatchObject({ view: "analyses" });
      expect(await screen.findByText("Orphaned blobs filled /var")).toBeInTheDocument();
    });

    // The view and its tile are /record readers. A role with only the
    // dashboard's own permission (`ro_stats`) gets a 403 from every one of
    // those calls, so the surfaces are not offered at all rather than offered
    // and broken — a tile that silently never appears, a view that shows an
    // error, and a 30-second refetch loop hammering a 403.
    describe("without permission to read records", () => {
      function mockStatsOnly() {
        const seen: string[] = [];
        mswServer.use(
          http.get("/api/v1/stats", () => HttpResponse.json(FULL_STATS_RESPONSE)),
          http.get("/api/v1/comment", () => HttpResponse.json(COMMENTS_RESPONSE)),
          http.get("/api/v1/record", ({ request }) => {
            seen.push(request.url);
            return HttpResponse.json({ errors: ["forbidden"] }, { status: 403 });
          }),
        );
        return seen;
      }

      beforeEach(() => {
        authStore.getState().logout();
        loginWithPerms(["ro_stats"]);
      });

      it("offers neither the Analysed tile nor the Analyses segment", async () => {
        const seen = mockStatsOnly();
        setup();
        await screen.findByText("Noise removed");

        expect(screen.queryByText("Analysed")).not.toBeInTheDocument();
        expect(screen.queryByRole("group", { name: "Dashboard view" })).not.toBeInTheDocument();
        // …and asks for nothing it would be refused.
        expect(seen.filter((u) => decodeQ(u).includes('"agentic"'))).toHaveLength(0);
      });

      it("falls back to the Overview on a ?view=analyses deep link", async () => {
        mockStatsOnly();
        setup("/web/dashboard?view=analyses");

        expect(await screen.findByText("Noise removed")).toBeInTheDocument();
        expect(screen.queryByText("No analyses yet")).not.toBeInTheDocument();
      });
    });
  });

  // W10: a payload missing the `by_notification` dimension entirely (not
  // just empty) must not throw — the panel falls back to its empty state.
  it("renders the Notifications empty state when the stats payload omits by_notification", async () => {
    const { by_notification: _drop, ...totalsWithoutNotification } =
      FULL_STATS_RESPONSE.data.totals;
    mswServer.use(
      http.get("/api/v1/stats", () =>
        HttpResponse.json({
          ...FULL_STATS_RESPONSE,
          data: { ...FULL_STATS_RESPONSE.data, totals: totalsWithoutNotification },
        }),
      ),
      http.get("/api/v1/comment", () => HttpResponse.json(COMMENTS_RESPONSE)),
    );
    const user = userEvent.setup();
    setup();
    await screen.findByText("Noise removed");

    await user.click(screen.getByRole("tab", { name: "Notifications" }));
    expect(await screen.findByText(/no notifications sent/i)).toBeInTheDocument();
  });

  // W12: an unparseable stats window must not degrade to a bogus 0-epoch
  // deep link — `from`/`to` should be absent from the href entirely.
  it("omits from/to from the notification deep link when the stats window can't be parsed", async () => {
    mswServer.use(
      http.get("/api/v1/stats", () =>
        HttpResponse.json({
          ...FULL_STATS_RESPONSE,
          meta: { ...FULL_STATS_RESPONSE.meta, from: "not-a-date", to: "not-a-date" },
        }),
      ),
      http.get("/api/v1/comment", () => HttpResponse.json(COMMENTS_RESPONSE)),
      http.get("/api/v1/notification", () =>
        HttpResponse.json({
          data: [{ uid: "nt-slack", name: "slack" }],
          meta: { count: 1, limit: 500, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await screen.findByText("Noise removed");

    await user.click(screen.getByRole("tab", { name: "Notifications" }));
    const link = await screen.findByRole("link", { name: "slack" });
    const href = link.getAttribute("href") ?? "";
    expect(href).toContain("details=nt-slack");
    expect(href).not.toContain("from=");
    expect(href).not.toContain("to=");
  });

  it("says which numbers are live and which are windowed", async () => {
    mswServer.use(
      http.get("/api/v1/stats", () => HttpResponse.json(FULL_STATS_RESPONSE)),
      http.get("/api/v1/comment", () => HttpResponse.json(COMMENTS_RESPONSE)),
    );
    setup();
    await screen.findByText("Noise removed");

    // The KPI clusters name their source…
    expect(screen.getByRole("region", { name: "Right now" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Last 24 hours" })).toBeInTheDocument();
    // …and every counter-backed panel repeats the window.
    expect(screen.getAllByText(/last 24 hours/i).length).toBeGreaterThanOrEqual(3);
    // The live panel says so instead.
    expect(screen.getByText(/right now, every alert/i)).toBeInTheDocument();
  });

  it("takes the headline live count from the same query as the alerts list", async () => {
    mswServer.use(
      http.get("/api/v1/stats", () => HttpResponse.json(FULL_STATS_RESPONSE)),
      http.get("/api/v1/comment", () => HttpResponse.json(COMMENTS_RESPONSE)),
      // The record store holds 5 rows in state=open (snapshot.open), but only
      // 2 of them are in the working queue — the rest are snoozed or shelved.
      // The tile must show the queue, like the sidebar badge and the table.
      // Only the limit=1 probe answers 2; the analysed page (the other /record
      // caller on this page) answers with a count of its own.
      http.get("/api/v1/record", ({ request }) =>
        decodeQ(request.url).includes('"ack"')
          ? HttpResponse.json({ data: [], meta: { count: 0, limit: 1, offset: 0, total: 2 } })
          : HttpResponse.json({ data: [], meta: { count: 0, limit: 1, offset: 0, total: 0 } }),
      ),
    );
    setup();
    const live = await screen.findByRole("region", { name: "Right now" });
    expect(within(live).getByText("2")).toBeInTheDocument();
    expect(within(live).queryByText("5")).not.toBeInTheDocument();
  });

  it("computes the suppressed share of the ingest stream", async () => {
    mswServer.use(
      http.get("/api/v1/stats", () => HttpResponse.json(FULL_STATS_RESPONSE)),
      http.get("/api/v1/comment", () => HttpResponse.json(COMMENTS_RESPONSE)),
    );
    setup();
    // by_throttled 2+1 plus by_snoozed 3 = 6 of 15 ingested = 40%.
    await screen.findByText("Noise removed");
    expect(screen.getByText("6")).toBeInTheDocument();
    expect(screen.getByText(/of 15 events suppressed · 40% of the stream/i)).toBeInTheDocument();
  });

  it("labels the top-N cap on a high-cardinality breakdown instead of truncating silently", async () => {
    const by_throttled: Record<string, number> = {};
    for (let i = 0; i < 15; i++) by_throttled[`rule${i}`] = 15 - i;
    mswServer.use(
      http.get("/api/v1/stats", () =>
        HttpResponse.json({
          ...FULL_STATS_RESPONSE,
          data: {
            ...FULL_STATS_RESPONSE.data,
            totals: { ...FULL_STATS_RESPONSE.data.totals, by_throttled },
          },
        }),
      ),
      http.get("/api/v1/comment", () => HttpResponse.json(COMMENTS_RESPONSE)),
    );
    setup();
    // 15 rules capped to 6 → the panel says so rather than looking complete.
    expect(await screen.findByText(/top 6 of 15/i)).toBeInTheDocument();
  });

  it("prefixes each pane title with its content icon", async () => {
    mswServer.use(
      http.get("/api/v1/stats", () => HttpResponse.json(FULL_STATS_RESPONSE)),
      http.get("/api/v1/comment", () => HttpResponse.json(COMMENTS_RESPONSE)),
    );
    setup();
    await screen.findByText("Noise removed");

    const iconFor = (title: string) =>
      screen.getByText(title).querySelector("use")?.getAttribute("href");

    expect(iconFor("Noise removed")).toBe("/web/icons.svg#icon-filter");
    expect(iconFor("Alerts over time")).toBe("/web/icons.svg#icon-activity");
    expect(iconFor("Recent activity")).toBe("/web/icons.svg#icon-message-square");
    expect(iconFor("By state")).toBe("/web/icons.svg#icon-check-circle");
    expect(iconFor("Breakdowns")).toBe("/web/icons.svg#icon-layers");
  });

  describe("honest empty states", () => {
    const emptyWith = (counters?: { enabled: boolean; present: boolean }) => {
      mswServer.use(
        http.get("/api/v1/stats", () =>
          HttpResponse.json({
            data: EMPTY_STATS_DATA,
            meta: {
              from: "",
              to: "",
              bucket: 3600,
              ...(counters ? { counters } : {}),
            },
          }),
        ),
        http.get("/api/v1/comment", () =>
          HttpResponse.json({ data: [], meta: { count: 0, limit: 15, offset: 0, total: 0 } }),
        ),
        http.get("/api/v1/environment", () => HttpResponse.json(ENV_RESPONSE)),
      );
    };

    it("distinguishes counters being switched off", async () => {
      emptyWith({ enabled: false, present: false });
      setup();
      expect(await screen.findAllByText(/counters are off/i)).not.toHaveLength(0);
      expect(screen.getAllByText(/metrics_enabled/i)[0]).toBeInTheDocument();
    });

    it("distinguishes a fresh install from a quiet window", async () => {
      emptyWith({ enabled: true, present: false });
      setup();
      expect(await screen.findAllByText(/no counters yet/i)).not.toHaveLength(0);
      expect(screen.getAllByText(/first alert is ingested/i)[0]).toBeInTheDocument();
    });

    it("says the window is quiet when counters do exist elsewhere", async () => {
      emptyWith({ enabled: true, present: true });
      setup();
      expect(await screen.findAllByText(/no events in this window/i)).not.toHaveLength(0);
      expect(screen.getAllByText(/try a wider range/i)[0]).toBeInTheDocument();
    });

    it("offers a retry when the stats request fails", async () => {
      mswServer.use(
        http.get("/api/v1/stats", () => HttpResponse.json({ detail: "boom" }, { status: 500 })),
        http.get("/api/v1/comment", () =>
          HttpResponse.json({ data: [], meta: { count: 0, limit: 15, offset: 0, total: 0 } }),
        ),
      );
      setup();
      expect(await screen.findByText(/couldn't load the dashboard/i)).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /try again/i })).toBeInTheDocument();
    });
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

describe("DashboardPage — loading + drill-downs + deltas", () => {
  it("shows the skeleton while stats are pending, not a lone spinner", () => {
    // No MSW handler resolves immediately → query stays pending.
    mswServer.use(
      http.get("/api/v1/stats", () => new Promise(() => {})),
      http.get("/api/v1/comment", () => HttpResponse.json(COMMENTS_RESPONSE)),
      http.get("/api/v1/environment", () => HttpResponse.json(ENV_RESPONSE)),
    );
    const { getByTestId } = setup();
    expect(getByTestId("dashboard-skeleton")).toBeInTheDocument();
  });

  it("drills a severity segment into ?search=severity = <label>", async () => {
    mockFullDashboard();
    const user = userEvent.setup();
    const { router } = setup();
    // Severity is the Breakdowns panel's default tab; its DistributionBar
    // renders a clickable legend row per label. Scope to the card so the
    // matching bar segment doesn't make it ambiguous.
    const sevCard = (await screen.findByText("Breakdowns")).closest("section")!;
    // Both the bar segment and the legend row fire onSegmentClick; click either.
    const criticalBtn = within(sevCard).getAllByRole("button", { name: /critical/ })[0]!;
    await user.click(criticalBtn);
    await waitFor(() => expect(router.state.location.pathname).toBe("/web/alerts"));
    expect((router.state.location.search as { search?: string }).search).toBe(
      "severity = critical",
    );
  });

  it("drills a state segment into exactly the rows it counted", async () => {
    mockFullDashboard();
    const user = userEvent.setup();
    const { router } = setup();
    // Scope to the "By state" card so we don't hit a KPI tile.
    const stateCard = (await screen.findByText("By state")).closest("section")!;
    // The legend shows the canonical noun ("Acknowledged"), but the segment
    // still drills down on the raw wire key ("ack") — label and display text
    // are decoupled via DistributionDatum.displayLabel.
    const ackBtn = within(stateCard).getAllByRole("button", { name: /Acknowledged/ })[0]!;
    await user.click(ackBtn);
    await waitFor(() => expect(router.state.location.pathname).toBe("/web/alerts"));
    // The "All" tab applies no lifecycle preset, so the DSL filter alone
    // decides the rows — and the list total matches the segment.
    const search = router.state.location.search as { tab?: string; search?: string };
    expect(search.tab).toBe("all");
    expect(search.search).toBe("state = ack");
  });

  it("drills an environment segment into ?env=<uid> resolved from the env list", async () => {
    mockFullDashboard();
    const user = userEvent.setup();
    const { router } = setup();
    const breakdowns = (await screen.findByText("Breakdowns")).closest("section")!;
    await user.click(within(breakdowns).getByRole("tab", { name: "Environment" }));
    // "prod" resolves to env-prod via the Environments list.
    const prodBtn = within(breakdowns).getAllByRole("button", { name: /prod/ })[0]!;
    await user.click(prodBtn);
    await waitFor(() => expect(router.state.location.pathname).toBe("/web/alerts"));
    expect((router.state.location.search as { env?: string }).env).toBe("env-prod");
  });

  it("navigates a live KPI tile to its lifecycle ?tab=", async () => {
    mockFullDashboard();
    const user = userEvent.setup();
    const { router } = setup();
    // Scope to the "Right now" tile cluster — the "By state" panel's legend
    // now also reads "Acknowledged" (the canonical noun, not the raw "ack"
    // key), so an unscoped query is ambiguous between the tile and the row.
    const live = await screen.findByRole("region", { name: "Right now" });
    await user.click(within(live).getByText("Acknowledged"));
    await waitFor(() => expect(router.state.location.pathname).toBe("/web/alerts"));
    expect((router.state.location.search as { tab?: string }).tab).toBe("ack");
  });

  it("renders a trend delta badge when the prior window differs", async () => {
    // Differentiate the two windows by their `from` query param: the current
    // window's `from` is ~1 day before now; the prior window's is ~2 days.
    const now = Date.now();
    mswServer.use(
      http.get("/api/v1/stats", ({ request }) => {
        const from = new URL(request.url).searchParams.get("from") ?? "";
        const ageDays = (now - Date.parse(from)) / 86_400_000;
        // Prior window (older `from`) reports fewer throttles than current.
        const throttled = ageDays > 1.5 ? { r: 2 } : { r: 4 };
        return HttpResponse.json({
          ...FULL_STATS_RESPONSE,
          data: {
            ...FULL_STATS_RESPONSE.data,
            totals: { ...FULL_STATS_RESPONSE.data.totals, by_throttled: throttled },
          },
        });
      }),
      http.get("/api/v1/comment", () => HttpResponse.json(COMMENTS_RESPONSE)),
      http.get("/api/v1/environment", () => HttpResponse.json(ENV_RESPONSE)),
    );
    setup();
    // current 4 vs prior 2 → +100%.
    expect(await screen.findByLabelText(/\+100% vs prior period/)).toBeInTheDocument();
  });
});

describe("alertsSearchForBucket", () => {
  it("returns a date_epoch range string for a given ISO bucket start and bucket size", () => {
    // 2026-05-14T00:00:00Z = 1747180800 (Unix)
    const x = "2026-05-14T00:00:00Z";
    const bucket = 3600;
    const from = Math.floor(Date.parse(x) / 1000);
    const to = from + bucket;
    expect(alertsSearchForBucket(x, bucket)).toBe(`date_epoch > ${from} and date_epoch < ${to}`);
  });

  it("handles a 6-hour bucket correctly", () => {
    const x = "2026-05-14T12:00:00Z";
    const bucket = 21600;
    const from = Math.floor(Date.parse(x) / 1000);
    const to = from + bucket;
    expect(alertsSearchForBucket(x, bucket)).toBe(`date_epoch > ${from} and date_epoch < ${to}`);
  });
});

describe("alertsSearchForRange", () => {
  it("spans from the first bucket start to one bucket past the last", () => {
    const fromX = "2026-05-14T00:00:00Z";
    const toX = "2026-05-14T03:00:00Z";
    const bucket = 3600;
    const from = Math.floor(Date.parse(fromX) / 1000);
    const to = Math.floor(Date.parse(toX) / 1000) + bucket;
    expect(alertsSearchForRange(fromX, toX, bucket)).toBe(
      `date_epoch > ${from} and date_epoch < ${to}`,
    );
  });

  it("matches alertsSearchForBucket when the range is a single bucket", () => {
    const x = "2026-05-14T00:00:00Z";
    const bucket = 3600;
    expect(alertsSearchForRange(x, x, bucket)).toBe(alertsSearchForBucket(x, bucket));
  });
});

// ── Screen-reader parity for the 30s poll ─────────────────────────────────

describe("DashboardPage — refresh announcements", () => {
  it("announces only when a background refresh moved Needs attention", async () => {
    let total = 2;
    mswServer.use(
      http.get("/api/v1/stats", () => HttpResponse.json(FULL_STATS_RESPONSE)),
      http.get("/api/v1/comment", () => HttpResponse.json(COMMENTS_RESPONSE)),
      http.get("/api/v1/record", ({ request }) =>
        new URL(request.url).searchParams.get("limit") === "1"
          ? HttpResponse.json({ data: [], meta: { count: 0, limit: 1, offset: 0, total } })
          : HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
    );
    const { client } = setup();
    const live = await screen.findByRole("region", { name: "Right now" });
    expect(within(live).getByText("2")).toBeInTheDocument();
    // Arriving at the page is not a change: first load stays silent.
    expect(screen.getByTestId("live-polite").textContent).toBe("");

    total = 4;
    await act(async () => {
      await client.refetchQueries();
    });
    await waitFor(() =>
      expect(screen.getByTestId("live-polite")).toHaveTextContent(
        "Dashboard refreshed. Needs attention: 4, was 2.",
      ),
    );
  });

  it("stays silent when a refresh leaves Needs attention where it was", async () => {
    mswServer.use(
      http.get("/api/v1/stats", () => HttpResponse.json(FULL_STATS_RESPONSE)),
      http.get("/api/v1/comment", () => HttpResponse.json(COMMENTS_RESPONSE)),
      http.get("/api/v1/record", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 1, offset: 0, total: 2 } }),
      ),
    );
    const { client } = setup();
    await screen.findByRole("region", { name: "Right now" });
    await act(async () => {
      await client.refetchQueries();
    });
    // Long enough to have caught an announcement had one been queued.
    await act(() => new Promise((r) => setTimeout(r, 500)));
    expect(screen.getByTestId("live-polite").textContent).toBe("");
  });
});
