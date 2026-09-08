import { render, screen } from "@testing-library/react";
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
import { NotificationsPanel } from "./NotificationsPanel";

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

function notificationsValidateSearch(raw: Record<string, unknown>): {
  tab?: string;
  details?: string;
  from?: number;
  to?: number;
} {
  const out: { tab?: string; details?: string; from?: number; to?: number } = {};
  if (typeof raw["tab"] === "string") out.tab = raw["tab"];
  if (typeof raw["details"] === "string") out.details = raw["details"];
  if (typeof raw["from"] === "number") out.from = raw["from"];
  if (typeof raw["to"] === "number") out.to = raw["to"];
  return out;
}

function setup() {
  const root = createRootRoute({ component: () => <Outlet /> });
  const notificationsRoute = createRoute({
    getParentRoute: () => root,
    path: "/web/notifications",
    component: () => <div>notifications page</div>,
    validateSearch: notificationsValidateSearch,
  });
  const dashboardRoute = createRoute({
    getParentRoute: () => root,
    path: "/web/dashboard",
    component: () => <TestPanel />,
  });
  const tree = root.addChildren([notificationsRoute, dashboardRoute]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: ["/web/dashboard"] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
    </QueryClientProvider>,
  );
}

let panelProps: Parameters<typeof NotificationsPanel>[0] = {
  byNotification: {},
  windowFrom: 1_700_000_000,
  windowTo: 1_700_003_600,
  windowLabel: "Last 24 hours",
};

function TestPanel() {
  return <NotificationsPanel {...panelProps} />;
}

const NOTIFICATION_LIST = {
  data: [
    { uid: "nt-slack", name: "slack" },
    { uid: "nt-mail", name: "mail-oncall" },
  ],
  meta: { count: 2, limit: 500, offset: 0, total: 2 },
};

describe("NotificationsPanel", () => {
  // The link's destination is the inspector's Deliveries tab, so the panel
  // only links for a role that can read the delivery log.
  beforeEach(() => loginWithPerms(["ro_notification", "ro_notificationlog"]));
  afterEach(() => authStore.getState().logout());

  it("ranks entries by count, descending", async () => {
    mswServer.use(http.get("/api/v1/notification", () => HttpResponse.json(NOTIFICATION_LIST)));
    panelProps = {
      byNotification: { slack: 3, "mail-oncall": 9 },
      windowFrom: 1_700_000_000,
      windowTo: 1_700_003_600,
      windowLabel: "Last 24 hours",
    };
    setup();

    const rows = await screen.findAllByRole("listitem");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent("mail-oncall");
    expect(rows[1]).toHaveTextContent("slack");
  });

  it("shows a Top N hint when there are more than 8 notifications", async () => {
    mswServer.use(http.get("/api/v1/notification", () => HttpResponse.json(NOTIFICATION_LIST)));
    const many: Record<string, number> = {};
    for (let i = 0; i < 10; i++) many[`n${i}`] = 10 - i;
    panelProps = {
      byNotification: many,
      windowFrom: 1_700_000_000,
      windowTo: 1_700_003_600,
      windowLabel: "Last 24 hours",
    };
    setup();

    expect(await screen.findByText("Top 8 of 10")).toBeInTheDocument();
    expect(screen.getAllByRole("listitem")).toHaveLength(8);
  });

  it("links a resolved name to the notification inspector with the window carried in search", async () => {
    mswServer.use(http.get("/api/v1/notification", () => HttpResponse.json(NOTIFICATION_LIST)));
    panelProps = {
      byNotification: { slack: 5 },
      windowFrom: 1_700_000_000,
      windowTo: 1_700_003_600,
      windowLabel: "Last 24 hours",
    };
    setup();

    const link = await screen.findByRole("link", { name: "slack" });
    const href = link.getAttribute("href") ?? "";
    expect(href).toContain("/web/notifications");
    expect(href).toContain("details=nt-slack");
    expect(href).toContain("tab=notifications");
    expect(href).toContain("from=1700000000");
    expect(href).toContain("to=1700003600");
  });

  it("renders an unresolved name as text with a 'no longer exists' hint", async () => {
    mswServer.use(http.get("/api/v1/notification", () => HttpResponse.json(NOTIFICATION_LIST)));
    panelProps = {
      byNotification: { "deleted-notifier": 2 },
      windowFrom: 1_700_000_000,
      windowTo: 1_700_003_600,
      windowLabel: "Last 24 hours",
    };
    setup();

    await screen.findByText("no longer exists");
    expect(screen.getByText("deleted-notifier")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /deleted-notifier/ })).not.toBeInTheDocument();
  });

  // W8: when the notification list query errors outright (as opposed to
  // succeeding with an empty/non-matching list), the uid map is empty for
  // every row — that must not be confused with "genuinely doesn't exist".
  it("renders plain names with no 'no longer exists' hint while the notification list query errors", async () => {
    mswServer.use(
      http.get("/api/v1/notification", () =>
        HttpResponse.json({ message: "boom" }, { status: 500 }),
      ),
    );
    panelProps = {
      byNotification: { slack: 5 },
      windowFrom: 1_700_000_000,
      windowTo: 1_700_003_600,
      windowLabel: "Last 24 hours",
    };
    setup();

    expect(await screen.findByText("slack")).toBeInTheDocument();
    expect(screen.queryByText("no longer exists")).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "slack" })).not.toBeInTheDocument();
  });

  // W12: when the dashboard couldn't parse its window, the panel is handed
  // no window props at all — the link must carry `details` but omit
  // `from`/`to` rather than pointing at a bogus 0-epoch window.
  it("omits from/to from the link when no window is given", async () => {
    mswServer.use(http.get("/api/v1/notification", () => HttpResponse.json(NOTIFICATION_LIST)));
    panelProps = {
      byNotification: { slack: 5 },
      windowLabel: "Last 24 hours",
    };
    setup();

    const link = await screen.findByRole("link", { name: "slack" });
    const href = link.getAttribute("href") ?? "";
    expect(href).toContain("details=nt-slack");
    expect(href).not.toContain("from=");
    expect(href).not.toContain("to=");
  });

  it("renders the empty state when nothing was sent", async () => {
    mswServer.use(http.get("/api/v1/notification", () => HttpResponse.json(NOTIFICATION_LIST)));
    panelProps = {
      byNotification: {},
      windowFrom: 1_700_000_000,
      windowTo: 1_700_003_600,
      windowLabel: "Last 24 hours",
      counters: { enabled: true, present: true },
    };
    setup();

    expect(await screen.findByRole("status")).toHaveTextContent(/no notifications sent/i);
  });

  // W3: a role without ro_notificationlog would land on an inspector with no
  // Deliveries tab, so the name is plain text and the name→uid lookup that
  // only exists to build the link is never fetched.
  it("renders plain names and fetches no notification list without ro_notificationlog", async () => {
    authStore.getState().logout();
    loginWithPerms(["ro_stats"]);
    let listRequests = 0;
    mswServer.use(
      http.get("/api/v1/notification", () => {
        listRequests += 1;
        return HttpResponse.json(NOTIFICATION_LIST);
      }),
    );
    panelProps = {
      byNotification: { slack: 5 },
      windowFrom: 1_700_000_000,
      windowTo: 1_700_003_600,
      windowLabel: "Last 24 hours",
    };
    setup();

    expect(await screen.findByText("slack")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "slack" })).not.toBeInTheDocument();
    expect(screen.queryByText("no longer exists")).not.toBeInTheDocument();
    expect(listRequests).toBe(0);
  });
});
