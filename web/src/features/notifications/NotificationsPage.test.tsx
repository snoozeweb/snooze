import { StrictMode } from "react";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
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
import { toastStore } from "@/shared/ui/toast/useToast";
import { NotificationsPage } from "./NotificationsPage";

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

function newClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

// Radix UI's BubbleInput (used by Select inside editors) calls ResizeObserver in jsdom.
beforeAll(() => {
  if (typeof window !== "undefined" && !window.ResizeObserver) {
    window.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

function setup(
  pathname = "/web/notifications",
  { strict = false, client = newClient() }: { strict?: boolean; client?: QueryClient } = {},
) {
  const root = createRootRoute({ component: () => <Outlet /> });
  const notifications = createRoute({
    getParentRoute: () => root,
    path: "/web/notifications",
    component: NotificationsPage,
  });
  const tree = root.addChildren([notifications]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: [pathname] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const ui = (
    <QueryClientProvider client={client}>
      <TooltipProvider delay={0}>
        <ToastProvider>
          <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
          <Toaster />
        </ToastProvider>
      </TooltipProvider>
    </QueryClientProvider>
  );
  // `strict` mounts under StrictMode, which double-invokes effects — the only
  // way to catch a toast that fires once per invocation.
  const utils = render(strict ? <StrictMode>{ui}</StrictMode> : ui);
  // The router is returned so the URL-addressable inspector tests can read
  // back what the page wrote to ?details / ?from / ?to — and re-navigate to a
  // key WITHOUT remounting, which is the only way to exercise the toast latch
  // across two visits inside one session.
  return {
    ...utils,
    router: router as unknown as {
      state: { location: { search: unknown } };
      navigate: (opts: { to: string; search: Record<string, unknown> }) => Promise<void>;
    },
  };
}

describe("NotificationsPage", () => {
  beforeEach(() => loginWithPerms(["ro_notification", "ro_notificationlog"]));
  afterEach(() => {
    authStore.getState().logout();
    toastStore.clear();
  });

  it("lists notifications in the Notifications tab", async () => {
    mswServer.use(
      http.get("/api/v1/notification", () =>
        HttpResponse.json({
          data: [{ uid: "n1", name: "Page on-call", enabled: true }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.get("/api/v1/action", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("Page on-call")).toBeInTheDocument());
    // The single-row Delete / Copy actions are discoverable via a visible kebab,
    // not just the invisible right-click menu.
    expect(screen.getByLabelText("Row actions")).toBeInTheDocument();
  });

  it("keeps the WHEN/HOW concept strip visible even once a tab has rows", async () => {
    mswServer.use(
      http.get("/api/v1/notification", () =>
        HttpResponse.json({
          data: [{ uid: "n1", name: "Page on-call", enabled: true }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.get("/api/v1/action", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("Page on-call")).toBeInTheDocument());
    // The mental-model strip is not gated on emptiness, so it survives a
    // populated list, and it links to the notifications docs.
    expect(screen.getByText(/decide/i)).toBeInTheDocument();
    const link = screen.getByRole("link", { name: /learn more/i });
    expect(link).toHaveAttribute(
      "href",
      "https://snoozeweb.github.io/snooze/general/notifications",
    );
  });

  it("switches to the Actions tab and lists actions", async () => {
    mswServer.use(
      http.get("/api/v1/notification", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
      http.get("/api/v1/action", () =>
        HttpResponse.json({
          data: [
            { uid: "a1", name: "Slack-prod", action: { selected: "webhook", subcontent: {} } },
          ],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await user.click(screen.getByRole("tab", { name: /actions/i }));
    await waitFor(() => expect(screen.getByText("Slack-prod")).toBeInTheDocument());
    expect(screen.getByLabelText("Row actions")).toBeInTheDocument();
  });

  it("selecting a row switches the toolbar to the amber bulk-actions chip on both tabs", async () => {
    mswServer.use(
      http.get("/api/v1/notification", () =>
        HttpResponse.json({
          data: [{ uid: "n1", name: "On critical", enabled: true }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.get("/api/v1/action", () =>
        HttpResponse.json({
          data: [
            { uid: "a1", name: "Slack-prod", action: { selected: "webhook", subcontent: {} } },
          ],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();

    await waitFor(() => expect(screen.getByText("On critical")).toBeInTheDocument());
    expect(screen.queryByRole("region", { name: /bulk actions/i })).not.toBeInTheDocument();
    await user.click(screen.getByRole("checkbox", { name: /select row/i }));
    expect(screen.getByRole("region", { name: /bulk actions/i })).toBeInTheDocument();
    expect(screen.getByText("1 selected")).toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: /actions/i }));
    await waitFor(() => expect(screen.getByText("Slack-prod")).toBeInTheDocument());
    expect(screen.queryByRole("region", { name: /bulk actions/i })).not.toBeInTheDocument();
    await user.click(screen.getByRole("checkbox", { name: /select row/i }));
    expect(screen.getByRole("region", { name: /bulk actions/i })).toBeInTheDocument();
    expect(screen.getByText("1 selected")).toBeInTheDocument();
  });

  // ---- URL-addressable row inspector (?details=) -------------------------

  function stubInspector() {
    mswServer.use(
      http.get("/api/v1/notification", () =>
        HttpResponse.json({
          data: [
            {
              uid: "n1",
              name: "Page on-call",
              enabled: true,
              actions: ["mail-oncall"],
              hits: 3,
              last_sent: 1757340000,
            },
          ],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.get("/api/v1/action", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
      http.get("/api/v1/notificationlog", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 10, offset: 0, total: 0 } }),
      ),
      http.get("/api/v1/audit", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 5, offset: 0, total: 0 } }),
      ),
    );
  }

  it("opens the row inspector straight from ?details= (deep link)", async () => {
    stubInspector();
    setup("/web/notifications?details=n1");
    // The inspector, not the editor: Deliveries leads and the counters read.
    expect(await screen.findByRole("tab", { name: "Deliveries" })).toBeVisible();
    expect(screen.getByText("Sent 3\u00d7")).toBeVisible();
  });

  it("clears details, from and to when the inspector is closed", async () => {
    stubInspector();
    const user = userEvent.setup();
    const { router } = setup("/web/notifications?details=n1&from=1757200000&to=1757400000");
    await screen.findByRole("tab", { name: "Deliveries" });
    expect((router.state.location.search as Record<string, unknown>)["details"]).toBe("n1");

    await user.click(screen.getByRole("button", { name: /close panel/i }));
    await waitFor(() => {
      const s = router.state.location.search as Record<string, unknown>;
      expect(s["details"]).toBeUndefined();
      expect(s["from"]).toBeUndefined();
      expect(s["to"]).toBeUndefined();
    });
  });

  it("passes ?from&to through to the Deliveries window chip", async () => {
    stubInspector();
    setup("/web/notifications?details=n1&from=1757200000&to=1757400000");
    await screen.findByRole("tab", { name: "Deliveries" });
    // The chip is dismissable, so its remove button names the window.
    expect(
      await screen.findByRole("button", { name: /remove window filter/i }),
    ).toBeInTheDocument();
  });

  // ---- deep link to a row that is not on the current page ----------------

  // Real uids are UUIDs (`uuid.NewString()` on the Go side) and the off-page
  // fallback only fires for keys shaped like one, so these fixtures use the
  // real shape rather than a short label.
  const OFFPAGE_UID = "3f2504e0-4f89-11d3-9a0c-0305e82c3301";
  const GONE_UID = "6ba7b810-9dad-11d1-80b4-00c04fd430c8";

  it("opens the inspector for a uid that is not in the current page's rows", async () => {
    // The controlled `detailsKey` only resolves against the rows the table
    // happens to be showing, so a dashboard link to a notification on page 2+
    // (or behind an active search) silently did nothing.
    let fetchedUid = "";
    mswServer.use(
      http.get("/api/v1/notification/:uid", ({ params }) => {
        fetchedUid = String(params["uid"]);
        return HttpResponse.json({
          uid: OFFPAGE_UID,
          name: "Deep linked",
          enabled: true,
          actions: ["mail-oncall"],
          hits: 7,
        });
      }),
      http.get("/api/v1/notification", () =>
        HttpResponse.json({
          data: [{ uid: "n1", name: "Page on-call", enabled: true }],
          meta: { count: 1, limit: 50, offset: 0, total: 40 },
        }),
      ),
      http.get("/api/v1/action", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
      http.get("/api/v1/notificationlog", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 10, offset: 0, total: 0 } }),
      ),
      http.get("/api/v1/audit", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 5, offset: 0, total: 0 } }),
      ),
    );
    setup(`/web/notifications?details=${OFFPAGE_UID}`);
    expect(await screen.findByRole("tab", { name: "Deliveries" })).toBeVisible();
    expect(screen.getByText("Sent 7\u00d7")).toBeVisible();
    expect(fetchedUid).toBe(OFFPAGE_UID);
  });

  it("clears the key and says so when the deep-linked uid no longer exists", async () => {
    mswServer.use(
      http.get("/api/v1/notification/:uid", () =>
        HttpResponse.json(
          { error: { code: "not_found", message: "no such notification" } },
          { status: 404 },
        ),
      ),
      http.get("/api/v1/notification", () =>
        HttpResponse.json({
          data: [{ uid: "n1", name: "Page on-call", enabled: true }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.get("/api/v1/action", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
    );
    const { router } = setup(`/web/notifications?details=${GONE_UID}`);
    expect(await screen.findByText("That notification no longer exists")).toBeVisible();
    await waitFor(() =>
      expect((router.state.location.search as Record<string, unknown>)["details"]).toBeUndefined(),
    );
  });

  function stubFallbackStatus(status: number, body: { error: { code: string; message: string } }) {
    mswServer.use(
      http.get("/api/v1/notification/:uid", () => HttpResponse.json(body, { status })),
      http.get("/api/v1/notification", () =>
        HttpResponse.json({
          data: [{ uid: "n1", name: "Page on-call", enabled: true }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.get("/api/v1/action", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
    );
  }

  it("reports a 403 on the off-page lookup and drops the key", async () => {
    // Only 404 used to be handled, so a role that can list notifications but
    // not read one (or any 5xx) produced nothing at all: no drawer, no toast,
    // and `?details=` left in the URL to be retried on every reload.
    stubFallbackStatus(403, {
      error: { code: "forbidden", message: "insufficient permissions" },
    });
    const { router } = setup(`/web/notifications?details=${GONE_UID}`);
    // The server's own words, in the app's voice — not "no longer exists",
    // which would be a lie about a row that is merely unreadable.
    expect(await screen.findByText("Insufficient permissions.")).toBeVisible();
    await waitFor(() =>
      expect((router.state.location.search as Record<string, unknown>)["details"]).toBeUndefined(),
    );
  });

  it("reports a 500 on the off-page lookup and drops the key", async () => {
    stubFallbackStatus(500, { error: { code: "internal", message: "boom" } });
    const { router } = setup(`/web/notifications?details=${GONE_UID}`);
    expect(await screen.findByText("The server ran into a problem.")).toBeVisible();
    await waitFor(() =>
      expect((router.state.location.search as Record<string, unknown>)["details"]).toBeUndefined(),
    );
  });

  it("shows a single dead-link toast, including under StrictMode", async () => {
    // One dead key must produce one banner. The effect is re-entrant by
    // construction — StrictMode double-invokes mount effects, and the key it
    // reacts to only clears a render later — so `toastedForKey` latches on the
    // uid. This mounts twice against one QueryClient (the second mount finds
    // the 404 already cached, the way a return visit to the same link does)
    // and counts the toasts rather than looking for the text.
    stubFallbackStatus(404, { error: { code: "not_found", message: "no such notification" } });
    const client = newClient();
    const first = setup(`/web/notifications?details=${GONE_UID}`, { client });
    await screen.findByText("That notification no longer exists");
    first.unmount();
    toastStore.clear();

    setup(`/web/notifications?details=${GONE_UID}`, { client, strict: true });
    await waitFor(() => expect(toastStore.getSnapshot().length).toBeGreaterThan(0));
    expect(toastStore.getSnapshot()).toHaveLength(1);
  });

  it("applies the same off-page fallback to ?actionDetails= on the Actions tab", async () => {
    // Actions live on this page too, behind their own key — the fallback,
    // the toast and the key-clearing are one shared code path, so a 404 on
    // the Actions tab must read "action", not "notification".
    mswServer.use(
      http.get("/api/v1/action/:uid", () =>
        HttpResponse.json(
          { error: { code: "not_found", message: "no such action" } },
          { status: 404 },
        ),
      ),
      http.get("/api/v1/notification", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
      http.get("/api/v1/action", () =>
        HttpResponse.json({
          data: [
            { uid: "a1", name: "Slack-prod", action: { selected: "webhook", subcontent: {} } },
          ],
          meta: { count: 1, limit: 50, offset: 0, total: 40 },
        }),
      ),
    );
    const { router } = setup(`/web/notifications?tab=actions&actionDetails=${GONE_UID}`);
    expect(await screen.findByText("That action no longer exists")).toBeVisible();
    await waitFor(() =>
      expect(
        (router.state.location.search as Record<string, unknown>)["actionDetails"],
      ).toBeUndefined(),
    );
  });

  it("reports an off-page non-uid details key as a dead end without asking the server", async () => {
    // `rowKey` falls back to `name` when a row has no uid. That key cannot be
    // resolved by `GET /notification/<uid>`, and trying anyway reported a
    // false "no longer exists" for a notification that is simply off-page — so
    // the lookup is suppressed. Suppressing it alone left the OTHER dead end
    // in place though: no drawer, no toast, `?details=` still in the URL. The
    // key has to be retired the same way a 404 is.
    let lookups = 0;
    mswServer.use(
      http.get("/api/v1/notification/:uid", () => {
        lookups += 1;
        return HttpResponse.json(
          { error: { code: "not_found", message: "no such notification" } },
          { status: 404 },
        );
      }),
      http.get("/api/v1/notification", () =>
        HttpResponse.json({
          data: [{ uid: "n1", name: "Page on-call", enabled: true }],
          meta: { count: 1, limit: 50, offset: 0, total: 40 },
        }),
      ),
      http.get("/api/v1/action", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
    );
    const { router } = setup("/web/notifications?details=Page%20me%20by%20name");
    await waitFor(() => expect(screen.getByText("Page on-call")).toBeInTheDocument());
    expect(await screen.findByText("That notification no longer exists")).toBeVisible();
    await waitFor(() =>
      expect((router.state.location.search as Record<string, unknown>)["details"]).toBeUndefined(),
    );
    expect(lookups).toBe(0);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("re-announces the same dead link on a second visit in the same session", async () => {
    // `toastedForKey` exists only to absorb StrictMode's double-invoke for ONE
    // key. Latching it for the lifetime of the page made the second visit to
    // the same dead uid silent — the toast fired, the key cleared, and every
    // later click on that stale bookmark did nothing at all.
    stubFallbackStatus(404, { error: { code: "not_found", message: "no such notification" } });
    const { router } = setup(`/web/notifications?details=${GONE_UID}`);

    expect(await screen.findByText("That notification no longer exists")).toBeVisible();
    await waitFor(() =>
      expect((router.state.location.search as Record<string, unknown>)["details"]).toBeUndefined(),
    );
    expect(toastStore.getSnapshot()).toHaveLength(1);
    toastStore.clear();

    // Same page, same key, no remount — the 404 is cached, so this is exactly
    // a user re-opening the stale link later on.
    await act(async () => {
      await router.navigate({ to: "/web/notifications", search: { details: GONE_UID } });
    });

    expect(await screen.findByText("That notification no longer exists")).toBeVisible();
    expect(toastStore.getSnapshot()).toHaveLength(1);
    await waitFor(() =>
      expect((router.state.location.search as Record<string, unknown>)["details"]).toBeUndefined(),
    );
  });

  // ---- per-tab inspector keys (?details= vs ?actionDetails=) --------------

  function stubBothTabs() {
    mswServer.use(
      http.get("/api/v1/notification", () =>
        HttpResponse.json({
          data: [{ uid: "n1", name: "Page on-call", enabled: true, actions: ["mail-oncall"] }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.get("/api/v1/action", () =>
        HttpResponse.json({
          data: [
            { uid: "a1", name: "Slack-prod", action: { selected: "webhook", subcontent: {} } },
          ],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.get("/api/v1/notificationlog", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 10, offset: 0, total: 0 } }),
      ),
      http.get("/api/v1/audit", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 5, offset: 0, total: 0 } }),
      ),
    );
  }

  it("ignores ?details= on the Actions tab and reads ?actionDetails= instead", async () => {
    // One shared key would open the Actions drawer keyed on a NOTIFICATION uid
    // — which resolves to no row, so the drawer just never appears.
    stubBothTabs();
    setup("/web/notifications?tab=actions&details=n1");
    await waitFor(() => expect(screen.getByText("Slack-prod")).toBeInTheDocument());
    expect(screen.queryByRole("tab", { name: "Deliveries" })).toBeNull();
  });

  it("opens the action inspector from ?actionDetails=", async () => {
    stubBothTabs();
    setup("/web/notifications?tab=actions&actionDetails=a1");
    expect(await screen.findByRole("tab", { name: "Deliveries" })).toBeVisible();
    // The action summary strip names its notifier ("webhook" also appears in
    // the table row behind the drawer, so scope to the drawer).
    expect(within(screen.getByRole("dialog")).getByText("webhook")).toBeVisible();
  });

  it("drops a stale inspector key and the deep-linked window when tabs are switched", async () => {
    // Reachable path: the operator is on Actions with a `details` left over
    // from a notifications deep link. Without clearing, switching back pops a
    // drawer they never asked for — and re-applies a window from another page.
    stubBothTabs();
    const user = userEvent.setup();
    const { router } = setup(
      "/web/notifications?tab=actions&details=n1&from=1757200000&to=1757400000",
    );
    await waitFor(() => expect(screen.getByText("Slack-prod")).toBeInTheDocument());

    await user.click(screen.getByRole("tab", { name: /^notifications$/i }));
    await waitFor(() => {
      const s = router.state.location.search as Record<string, unknown>;
      expect(s["details"]).toBeUndefined();
      expect(s["from"]).toBeUndefined();
      expect(s["to"]).toBeUndefined();
    });
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("ignores a degenerate 0/0 window rather than filtering the timeline to nothing", async () => {
    // An unparseable dashboard range used to arrive as from=0&to=0, which the
    // timeline dutifully applied — an empty list behind a "— – —" chip.
    stubBothTabs();
    setup("/web/notifications?details=n1&from=0&to=0");
    await screen.findByRole("tab", { name: "Deliveries" });
    expect(screen.queryByRole("button", { name: /remove window filter/i })).toBeNull();
  });
});
