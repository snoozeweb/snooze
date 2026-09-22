import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";
import { toastStore } from "@/shared/ui/toast/useToast";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { mswServer } from "@/tests/msw/server";
import { LiveAnnouncerProvider } from "@/shared/a11y/LiveAnnouncer";
import { decodeConditionQ } from "@/lib/condition/decode";
import { authStore } from "@/lib/auth/store";
import { AlertsPage } from "./AlertsPage";
import { SILENCE_DESCRIPTIONS } from "./silencingGuide";

function setup(pathname = "/web/alerts", { withSnoozesStub = false } = {}) {
  const root = createRootRoute({ component: () => <Outlet /> });
  const alerts = createRoute({
    getParentRoute: () => root,
    path: "/web/alerts",
    component: AlertsPage,
  });
  // Only the "Snooze this alert" navigation test needs a real destination
  // route to land on — every other test leaves the tree minimal.
  const snoozesStub = createRoute({
    getParentRoute: () => root,
    path: "/web/snoozes",
    component: () => <div>snoozes stub</div>,
  });
  const tree = withSnoozesStub
    ? root.addChildren([alerts, snoozesStub])
    : root.addChildren([alerts]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: [pathname] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        {/* Mirrors the real tree (router.tsx mounts this at the root) so the
            page's refresh announcements land in a live region here too. */}
        <LiveAnnouncerProvider>
          {/* router is locally constructed; cast needed for the registered-router type mismatch */}
          <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
        </LiveAnnouncerProvider>
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return router;
}

describe("AlertsPage", () => {
  afterEach(() => {
    // Toasts live in a module-level store; clear it so undo-toast assertions
    // in one test don't leak into the next.
    toastStore.clear();
  });

  it("renders rows from /api/v1/record", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            {
              uid: "r1",
              host: "srv-1",
              severity: "critical",
              state: "open",
              message: "disk full",
              date_epoch: Math.floor(Date.now() / 1000),
            },
          ],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    expect(screen.getByText(/disk full/)).toBeInTheDocument();
  });

  it("offers an Acknowledge action on open rows that POSTs to /record/bulk_state via dialog", async () => {
    const bulkCalls: Array<{ url: string; body: unknown }> = [];
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", severity: "info", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.post("/api/v1/record/bulk_state", async ({ request }) => {
        bulkCalls.push({ url: request.url, body: await request.json() });
        return HttpResponse.json({ matched: 1, updated: 1, state: "ack" });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
    await user.click(screen.getByRole("menuitem", { name: /acknowledge/i }));
    // Dialog should appear; confirm it
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: /^acknowledge$/i }));
    await waitFor(() => expect(bulkCalls.length).toBe(1));
    // body should have state:"ack"
    expect(bulkCalls[0]!.body).toMatchObject({ state: "ack" });
    // q param decodes to uid IN ["r1"]
    const rawQ = new URL(bulkCalls[0]!.url).searchParams.get("q") ?? "";
    const decoded = JSON.parse(atob(rawQ.replace(/-/g, "+").replace(/_/g, "/"))) as unknown;
    expect(decoded).toMatchObject({ type: "IN", field: "uid", value: ["r1"] });
  });

  it("the 'View details' quick action opens the detail drawer on Timeline, with the JSON behind the Record tab", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            {
              uid: "r1",
              host: "srv-1",
              severity: "info",
              state: "open",
              message: "boom",
              date_epoch: 1,
            },
          ],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.get("/api/v1/comment", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 100, offset: 0, total: 0 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    // No drawer on the bare list.
    expect(screen.queryByRole("dialog")).toBeNull();
    // Row click no longer opens the drawer — the hover-revealed "View
    // details" quick action does (it wires DataTable's onOpenDetails to set
    // ?record=). The drawer is a Radix Dialog → role=dialog.
    await user.click(screen.getByRole("button", { name: "View details" }));
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());
    // Timeline is the default tab — its empty state renders immediately.
    await waitFor(() => expect(screen.getByText(/no comments yet/i)).toBeInTheDocument());
    // The raw record lives behind the Record tab — click it to see the JSON
    // (the alert uid only appears there, not in the table).
    await user.click(screen.getByRole("tab", { name: /^record$/i }));
    expect(screen.getByText(/r1/)).toBeInTheDocument();
  });

  // The dashboard's Analyses panel links here: ?record= opens the inspector,
  // ?analysis= says which tab it opens on. Without the second param the deep
  // link would land on Timeline and bury the thing the operator clicked for.
  it("opens the inspector on the Analysis tab for ?record=…&analysis=1", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            {
              uid: "r1",
              host: "srv-1",
              severity: "info",
              state: "open",
              message: "boom",
              date_epoch: 1,
            },
          ],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.get("/api/v1/comment", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 100, offset: 0, total: 0 } }),
      ),
    );
    setup("/web/alerts?record=r1&analysis=1");

    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());
    const analysisTab = await screen.findByRole("tab", { name: /^analysis/i });
    expect(analysisTab).toHaveAttribute("aria-selected", "true");
    // The default tab is NOT the one selected.
    expect(screen.getByRole("tab", { name: /^timeline$/i })).toHaveAttribute(
      "aria-selected",
      "false",
    );
    // The panel is the analysis surface (this alert carries none — the msw
    // default answers the agentic GET with a 404).
    expect(await screen.findByText(/no analysis yet/i)).toBeInTheDocument();
  });

  it("bulk acknowledge: fires one POST to /record/bulk_state, shows count toast", async () => {
    const bulkCalls: Array<{ url: string; body: unknown }> = [];
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            { uid: "r1", host: "srv-1", state: "open", date_epoch: 1 },
            { uid: "r2", host: "srv-2", state: "open", date_epoch: 2 },
          ],
          meta: { count: 2, limit: 50, offset: 0, total: 2 },
        }),
      ),
      http.post("/api/v1/record/bulk_state", async ({ request }) => {
        bulkCalls.push({ url: request.url, body: await request.json() });
        return HttpResponse.json({ matched: 2, updated: 2, state: "ack" });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());

    await user.click(screen.getByRole("checkbox", { name: /select all/i }));
    await user.click(screen.getByRole("button", { name: /acknowledge \(2\)/i }));
    await user.click(screen.getByRole("button", { name: /^acknowledge$/i }));

    // One bulk call, not two comment calls
    await waitFor(() => expect(bulkCalls).toHaveLength(1));
    expect(bulkCalls[0]!.body).toMatchObject({ state: "ack" });

    // Success toast shows matched count
    await waitFor(() => {
      const toasts = toastStore.getSnapshot();
      expect(toasts.some((t) => /2 alerts updated/i.test(t.description))).toBe(true);
    });
  });

  it("comment-only: requires a message and posts type=comment", async () => {
    const calls: unknown[] = [];
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.post("/api/v1/comment", async ({ request }) => {
        calls.push(await request.json());
        return HttpResponse.json({ ok: true });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());

    await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
    await user.click(screen.getByRole("menuitem", { name: /^comment$/i }));
    // Try submitting with empty message — should be blocked.
    await user.click(screen.getByRole("button", { name: /^comment$/i }));
    expect(calls).toHaveLength(0);

    await user.type(screen.getByPlaceholderText(/type your comment/i), "investigating");
    await user.click(screen.getByRole("button", { name: /^comment$/i }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toMatchObject({ record_uid: "r1", type: "comment", message: "investigating" });
  });

  it("surfaces the row keyboard shortcuts in a discoverable legend", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: /keyboard shortcuts/i }));
    // The legend teaches both the built-in navigation keys and the alert-row
    // bindings (a=ack, c=comment) that were previously undiscoverable.
    expect(await screen.findByText("Move between rows")).toBeInTheDocument();
    expect(screen.getByText(/acknowledge focused alert/i)).toBeInTheDocument();
    expect(screen.getByText(/comment on focused alert/i)).toBeInTheDocument();
  });

  it("right-click context menu shows Copy/Acknowledge/Comment/Delete items", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    const row = screen.getByText("srv-1").closest("tr")!;
    await user.pointer({ keys: "[MouseRight]", target: row });
    await waitFor(() =>
      expect(screen.getByRole("menu", { name: /row context menu/i })).toBeInTheDocument(),
    );
    expect(screen.getByRole("menuitem", { name: /copy as json/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /copy as yaml/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /^acknowledge$/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /^comment$/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /^delete$/i })).toBeInTheDocument();
  });

  it("context-menu Delete asks for confirmation then DELETEs /record/<uid>", async () => {
    const dels: string[] = [];
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.delete("/api/v1/record/:uid", ({ params }) => {
        dels.push(String(params.uid));
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    const row = screen.getByText("srv-1").closest("tr")!;
    await user.pointer({ keys: "[MouseRight]", target: row });
    await user.click(screen.getByRole("menuitem", { name: /^delete$/i }));
    // Confirm dialog appears.
    await waitFor(() => expect(screen.getByText(/Delete alert\?/i)).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: /^delete$/i }));
    await waitFor(() => expect(dels).toEqual(["r1"]));
  });

  // Deep-link from the snooze-teams alert card: the host hyperlink points at
  // /web/alerts?search=hash%20%3D%20<hash>. The page must seed the SearchBar
  // from the URL on mount so the operator lands on the filtered view rather
  // than the full alerts list.
  it("seeds the SearchBar from ?search= on initial navigation", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
    );
    setup("/web/alerts?search=hash%20%3D%20abc123");
    // SearchBar's <input> defaults to aria-label="Search" (see
    // shared/ui/SearchBar.tsx:312); querying by that role is more stable
    // than the placeholder text which can change.
    const input = await screen.findByRole("textbox", { name: /search/i });
    expect((input as HTMLInputElement).value).toBe("hash = abc123");
  });

  it("genuinely-empty list shows the inject CTA and opens the dialog", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText(/no alerts yet/i)).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: /how to inject alerts/i }));
    await waitFor(() =>
      expect(screen.getByRole("dialog", { name: /how to inject alerts/i })).toBeInTheDocument(),
    );
    expect(screen.getByText(/curl -s -X POST/)).toBeInTheDocument();
  });

  it("earned-empty queue (alerts were ingested, all triaged) shows All clear, not onboarding", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
      http.get("/api/v1/stats", () =>
        HttpResponse.json({
          data: { series: [], totals: {}, snapshot: {}, weekday: {} },
          meta: { from: "", to: "", bucket: 86400, counters: { enabled: true, present: true } },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText(/all clear/i)).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: /how to inject alerts/i })).toBeNull();
  });

  it("a failed record query shows the error panel — never 'All clear'", async () => {
    // The regression this guards: with /record 500ing, the page rendered the
    // green "All clear — every alert has been triaged" card, i.e. it reported
    // an empty queue it had never managed to read.
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({ error: { code: "internal" } }, { status: 500 }),
      ),
      http.get("/api/v1/stats", () =>
        HttpResponse.json({
          data: { series: [], totals: {}, snapshot: {}, weekday: {} },
          meta: { from: "", to: "", bucket: 86400, counters: { enabled: true, present: true } },
        }),
      ),
    );
    setup();
    await waitFor(() =>
      expect(screen.getByText(/can't reach the alert store/i)).toBeInTheDocument(),
    );
    expect(screen.queryByText(/all clear/i)).toBeNull();
    expect(screen.getByText(/no data loaded/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /try again/i })).toBeInTheDocument();
    // The toolbar says it too, loudly enough to see from the refresh control.
    expect(screen.getByRole("button", { name: /not updating/i })).toBeInTheDocument();
  });

  it("the error panel's Try again refetches and restores the list", async () => {
    let fail = true;
    mswServer.use(
      http.get("/api/v1/record", () => {
        if (fail) return HttpResponse.json({ error: { code: "internal" } }, { status: 500 });
        return HttpResponse.json({
          data: [{ uid: "r1", host: "srv-recovered", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() =>
      expect(screen.getByText(/can't reach the alert store/i)).toBeInTheDocument(),
    );
    fail = false;
    await user.click(screen.getByRole("button", { name: /try again/i }));
    await waitFor(() => expect(screen.getByText("srv-recovered")).toBeInTheDocument());
    expect(screen.queryByText(/can't reach the alert store/i)).toBeNull();
  });

  it("filtered-empty list shows a no-match message, not the inject CTA", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
    );
    setup("/web/alerts?search=host%20%3D%20nope");
    await waitFor(() =>
      expect(screen.getByText(/no alerts match your filters/i)).toBeInTheDocument(),
    );
    expect(screen.queryByRole("button", { name: /how to inject alerts/i })).toBeNull();
  });

  // ── Phase 5: inline quick actions + undo ──────────────────────────────────

  it("renders inline quick-action buttons (ack/close/comment) on open rows", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", severity: "info", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    // quickActions render as IconButtons (aria-label = action label). They're
    // always in the DOM (hover/focus only toggles opacity via CSS), so DOM
    // presence is the right assertion in jsdom.
    expect(screen.getByRole("button", { name: /^acknowledge$/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^close alert$/i })).toBeInTheDocument();
    // Two "Comment" buttons would collide with the kebab menu item, so the
    // quick-action one is scoped to its IconButton role+name.
    expect(screen.getAllByRole("button", { name: /^comment$/i }).length).toBeGreaterThan(0);
  });

  it("inline ack POSTs type=ack directly (no dialog) and shows an Undo toast", async () => {
    const calls: Array<{ record_uid: string; type: string }> = [];
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", severity: "info", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.post("/api/v1/comment", async ({ request }) => {
        calls.push((await request.json()) as { record_uid: string; type: string });
        return HttpResponse.json({ ok: true });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());

    // Click the inline ack quick-action (the IconButton, NOT a menu item).
    await user.click(screen.getByRole("button", { name: /^acknowledge$/i }));

    // No confirm dialog — the inline path skips it.
    expect(screen.queryByRole("dialog")).toBeNull();
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toMatchObject({ record_uid: "r1", type: "ack" });

    // An undo toast was raised.
    await waitFor(() => {
      const toasts = toastStore.getSnapshot();
      expect(toasts.some((t) => /acknowledged srv-1/i.test(t.description) && t.action)).toBe(true);
    });
  });

  it("the Undo toast action POSTs a compensating type=open", async () => {
    const calls: Array<{ record_uid: string; type: string }> = [];
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", severity: "info", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.post("/api/v1/comment", async ({ request }) => {
        calls.push((await request.json()) as { record_uid: string; type: string });
        return HttpResponse.json({ ok: true });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: /^acknowledge$/i }));
    await waitFor(() => expect(calls).toHaveLength(1));

    // Fire the toast's Undo action directly (the Toaster isn't mounted here).
    const undoToast = await waitFor(() => {
      const t = toastStore.getSnapshot().find((x) => x.action);
      expect(t).toBeTruthy();
      return t!;
    });
    act(() => undoToast.action!.onSelect());

    await waitFor(() => expect(calls).toHaveLength(2));
    // Compensating event: the re-open is appended, the ack stays on record.
    expect(calls[1]).toMatchObject({ record_uid: "r1", type: "open" });
  });

  it("keyboard 'a' on a focused open row opens the Acknowledge confirm dialog", async () => {
    const calls: Array<{ q?: string; state?: string }> = [];
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", severity: "info", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.post("/api/v1/record/bulk_state", async ({ request }) => {
        calls.push((await request.json()) as { q?: string; state?: string });
        return HttpResponse.json({ matched: 1, updated: 1 });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());

    // Focus the grid, move to the first row (ArrowDown), then press 'a'. A
    // keystroke lands on whatever row the focus ring is on, so it confirms
    // rather than firing straight through like the mouse quick-action does.
    const grid = screen.getByRole("grid");
    grid.focus();
    await user.keyboard("{ArrowDown}a");

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/acknowledge alert/i)).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: /^acknowledge$/i }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toMatchObject({ state: "ack" });
  });

  it("keyboard 'c' on a focused open row opens the Close confirm dialog", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", severity: "info", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());

    const grid = screen.getByRole("grid");
    grid.focus();
    await user.keyboard("{ArrowDown}c");

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/close alert/i)).toBeInTheDocument();
  });

  it("keyboard 'f' expands the focused row's pipeline flow inline", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            {
              uid: "r1",
              host: "srv-1",
              state: "open",
              date_epoch: 1,
              source: "prometheus",
              rules: ["tag-prod"],
            },
          ],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());

    const grid = screen.getByRole("grid");
    grid.focus();
    await user.keyboard("{ArrowDown}f");

    // The SAME AlertFlowChart the drawer's Flow tab renders, hung under the
    // row — its stage labels are unique to that component.
    expect(await screen.findByText("Aggregate")).toBeInTheDocument();
    expect(screen.getByText("Input")).toBeInTheDocument();
    expect(screen.getByText("tag-prod")).toBeInTheDocument();

    // …and f again collapses it.
    await user.keyboard("f");
    await waitFor(() => expect(screen.queryByText("Aggregate")).toBeNull());
  });

  // ── Action gating ─────────────────────────────────────────────────────────

  it("ack_hidden_for_acked_rows — Acknowledge absent from kebab on acked row", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "ack", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
    expect(screen.queryByRole("menuitem", { name: /^acknowledge$/i })).toBeNull();
  });

  it("close_available_for_acked_rows — Close present in kebab on acked row", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "ack", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
    expect(screen.getByRole("menuitem", { name: /^close\b/i })).toBeInTheDocument();
  });

  it("esc_available_for_acked_rows — Re-escalate present in kebab on acked row", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "ack", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
    expect(screen.getByRole("menuitem", { name: /re-escalate/i })).toBeInTheDocument();
  });

  it("reopen_only_for_closed_rows — Re-open present, Acknowledge and Close absent on closed row", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "close", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
    expect(screen.getByRole("menuitem", { name: /re-open/i })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: /^acknowledge$/i })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: /^close\b/i })).toBeNull();
  });

  it("esc_hidden_for_fresh_rows — Re-escalate absent from kebab on fresh row", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
    expect(screen.queryByRole("menuitem", { name: /re-escalate/i })).toBeNull();
  });

  it("bulk_ack_disabled_when_all_closed — Acknowledge stays on the bar, disabled with a reason", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "close", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getByRole("checkbox", { name: /select all/i }));
    // The verb never vanishes — it reports 0 eligible and refuses, so the
    // operator learns why instead of watching the button disappear.
    const ack = screen.getByRole("button", { name: /acknowledge \(0 of 1\)/i });
    expect(ack).toHaveAttribute("aria-disabled", "true");
    await user.click(ack);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("quick_ack_absent_for_acked_row — inline Acknowledge icon-button absent on acked row", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "ack", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    // The inline Acknowledge quick-action button should not be in the DOM for an acked row
    expect(screen.queryByRole("button", { name: /^acknowledge$/i })).toBeNull();
  });

  // ── Columns: lifecycle countdown + trend ───────────────────────────────────
  // acked_by / escalate_hint / trend no longer render as their own columns —
  // the escalation countdown lives as a hint inside the State cell and the
  // trend arrow rides inside the Sev cell (see columns.tsx). Ack metadata
  // (who + until when) deliberately does NOT render in the table: it never
  // fit legibly in a column-width hint; the detail drawer carries it.

  it("acked_row_has_no_state_hint — ack owner/expiry live in the drawer, not the cell", async () => {
    const nowSec = Math.floor(Date.now() / 1000);
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            {
              uid: "r1",
              host: "srv-1",
              state: "ack",
              acked_by: "alice",
              ack_until: nowSec + 10800, // 3 hours
              date_epoch: 1,
            },
          ],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    expect(screen.queryByText(/in \d+h/)).toBeNull();
    expect(screen.queryByText("alice")).toBeNull();
  });

  it("escalate_hint_renders_on_open_row — shows escalation countdown on open row", async () => {
    const nowSec = Math.floor(Date.now() / 1000);
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            {
              uid: "r1",
              host: "srv-1",
              state: "",
              escalate_at: nowSec + 7200, // 2 hours
              date_epoch: 1,
            },
          ],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    expect(screen.getByTitle(/auto-escalation deadline/i)).toBeInTheDocument();
  });

  it("escalate_hint_absent_on_acked_row — no escalation hint when row is acked", async () => {
    const nowSec = Math.floor(Date.now() / 1000);
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            {
              uid: "r1",
              host: "srv-1",
              state: "ack",
              escalate_at: nowSec + 7200,
              acked_by: "alice",
              date_epoch: 1,
            },
          ],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    expect(screen.queryByTitle(/auto-escalation deadline/i)).toBeNull();
  });

  it("trend_up_renders_for_moreSevere — ↑ shown for moreSevere", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            {
              uid: "r1",
              host: "srv-1",
              state: "open",
              trend_indication: "moreSevere",
              date_epoch: 1,
            },
          ],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    expect(screen.getByTitle("Severity escalated")).toBeInTheDocument();
  });

  it("trend_down_renders_for_lessSevere — ↓ shown for lessSevere", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            {
              uid: "r1",
              host: "srv-1",
              state: "open",
              trend_indication: "lessSevere",
              date_epoch: 1,
            },
          ],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    expect(screen.getByTitle("Severity decreased")).toBeInTheDocument();
  });

  it("trend_arrow_absent_for_noChange_or_absent — no ↑/↓ arrow rendered in the Sev cell", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            { uid: "r1", host: "srv-1", state: "open", date_epoch: 1 },
            {
              uid: "r2",
              host: "srv-2",
              state: "open",
              trend_indication: "noChange",
              date_epoch: 2,
            },
          ],
          meta: { count: 2, limit: 50, offset: 0, total: 2 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    expect(screen.getByText("srv-2")).toBeInTheDocument();
    // No arrow (and no "—" placeholder that used to occupy its own trend
    // column) renders for absent/noChange trend — the Sev cell shows just
    // the severity badge.
    expect(screen.queryByTitle("Severity escalated")).toBeNull();
    expect(screen.queryByTitle("Severity decreased")).toBeNull();
  });

  it("shows the ActiveFilters chip strip with a non-default tab and Clear all", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
    );
    setup("/web/alerts?tab=ack");
    await waitFor(() =>
      expect(screen.getByRole("group", { name: /active filters/i })).toBeInTheDocument(),
    );
    const strip = screen.getByRole("group", { name: /active filters/i });
    expect(strip).toHaveTextContent(/acknowledged/i);
    expect(screen.getByRole("button", { name: /clear all/i })).toBeInTheDocument();
  });

  // ── Phase 1: state-transition parity with the backend ─────────────────────

  async function openKebab(state: string) {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", severity: "info", state, date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
    await waitFor(() => expect(screen.getByRole("menu")).toBeInTheDocument());
    return user;
  }

  it("escalated rows are actionable: kebab offers Acknowledge/Close/Re-open, never Re-escalate", async () => {
    await openKebab("esc");
    expect(screen.getByRole("menuitem", { name: /^acknowledge$/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /^close\b/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /^re-open$/i })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: /re-escalate/i })).toBeNull();
  });

  it("re-opened rows never offer Re-escalate (the button the backend always 403s)", async () => {
    await openKebab("open");
    expect(screen.getByRole("menuitem", { name: /^acknowledge$/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /^close\b/i })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: /re-escalate/i })).toBeNull();
  });

  it("acknowledged rows offer Re-open (previously missing)", async () => {
    await openKebab("ack");
    expect(screen.getByRole("menuitem", { name: /^re-open$/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /^close\b/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /re-escalate/i })).toBeInTheDocument();
  });

  it("bulk Re-escalate is hidden when no selected row can be escalated", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            { uid: "r1", host: "srv-1", state: "open", date_epoch: 1 },
            { uid: "r2", host: "srv-2", state: "open", date_epoch: 2 },
          ],
          meta: { count: 2, limit: 50, offset: 0, total: 2 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getByRole("checkbox", { name: /select all/i }));
    // Open rows can be acked/closed but not re-escalated (that's an ack-only
    // transition), so the bulk Re-escalate button must not appear.
    expect(screen.getByRole("button", { name: /acknowledge \(2\)/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /close \(2\)/i })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /re-escalate/i })).toBeNull();
  });

  // ── Plan 18b: bulk state via bulk_state endpoint ───────────────────────────

  it("bulk ack fires one POST to /record/bulk_state, zero to /comment", async () => {
    const bulkCalls: Array<{ url: string; body: unknown }> = [];
    const commentCalls: unknown[] = [];
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            { uid: "r1", host: "srv-1", state: "open", date_epoch: 1 },
            { uid: "r2", host: "srv-2", state: "open", date_epoch: 2 },
          ],
          meta: { count: 2, limit: 50, offset: 0, total: 2 },
        }),
      ),
      http.post("/api/v1/record/bulk_state", async ({ request }) => {
        bulkCalls.push({ url: request.url, body: await request.json() });
        return HttpResponse.json({ matched: 2, updated: 2, state: "ack" });
      }),
      http.post("/api/v1/comment", async ({ request }) => {
        commentCalls.push(await request.json());
        return HttpResponse.json({ ok: true });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getByRole("checkbox", { name: /select all/i }));
    await user.click(screen.getByRole("button", { name: /acknowledge \(2\)/i }));
    await user.click(screen.getByRole("button", { name: /^acknowledge$/i }));
    await waitFor(() => expect(bulkCalls).toHaveLength(1));
    expect(commentCalls).toHaveLength(0);
    // q decodes to uid IN ["r1","r2"]
    const rawQ = new URL((bulkCalls[0] as { url: string }).url).searchParams.get("q") ?? "";
    const decoded = JSON.parse(atob(rawQ.replace(/-/g, "+").replace(/_/g, "/"))) as unknown;
    expect(decoded).toMatchObject({
      type: "IN",
      field: "uid",
      // eslint-disable-next-line @typescript-eslint/no-unsafe-assignment
      value: expect.arrayContaining(["r1", "r2"]),
    });
  });

  it("bulk ack warns about no per-record comment in the toast", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            { uid: "r1", host: "srv-1", state: "open", date_epoch: 1 },
            { uid: "r2", host: "srv-2", state: "open", date_epoch: 2 },
          ],
          meta: { count: 2, limit: 50, offset: 0, total: 2 },
        }),
      ),
      http.post("/api/v1/record/bulk_state", () => {
        return HttpResponse.json({ matched: 2, updated: 2, state: "ack" });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getByRole("checkbox", { name: /select all/i }));
    await user.click(screen.getByRole("button", { name: /acknowledge \(2\)/i }));
    await user.click(screen.getByRole("button", { name: /^acknowledge$/i }));
    await waitFor(() => {
      const toasts = toastStore.getSnapshot();
      expect(toasts.some((t) => /2 alerts updated/i.test(t.description))).toBe(true);
      expect(toasts.some((t) => /no per-alert activity/i.test(t.description ?? ""))).toBe(true);
    });
  });

  it("bulk_state failure surfaces the backend's reason, not a generic message", async () => {
    // The single bulk_state call either succeeds or fails wholesale; when it
    // 403s (e.g. an invalid transition or missing permission) the error toast
    // must carry the backend's detail so the operator knows why — not the bare
    // "Bulk action failed" fallback. Regression guard for the merge that
    // replaced main's per-record bulk loop with plan 18b's single bulk_state.
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            { uid: "r1", host: "srv-1", state: "open", date_epoch: 1 },
            { uid: "r2", host: "srv-2", state: "open", date_epoch: 2 },
          ],
          meta: { count: 2, limit: 50, offset: 0, total: 2 },
        }),
      ),
      http.post("/api/v1/record/bulk_state", () =>
        HttpResponse.json(
          { error: { code: "invalid_transition", message: "alert already acknowledged" } },
          { status: 403 },
        ),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getByRole("checkbox", { name: /select all/i }));
    await user.click(screen.getByRole("button", { name: /acknowledge \(2\)/i }));
    await user.click(screen.getByRole("button", { name: /^acknowledge$/i }));

    // The backend's detail surfaces in the error toast...
    await waitFor(() => {
      const toasts = toastStore.getSnapshot();
      expect(toasts.some((t) => /alert already acknowledged/i.test(t.description ?? ""))).toBe(
        true,
      );
    });
    // ...and NOT the generic fallback.
    expect(toastStore.getSnapshot().some((t) => t.description === "Bulk action failed")).toBe(
      false,
    );
  });

  it('"Select all N" affordance appears when total > page size and rows selected', async () => {
    // Build 50 rows for the page, with total=200
    const rows = Array.from({ length: 50 }, (_, i) => ({
      uid: `r${i}`,
      host: `srv-${i}`,
      state: "open",
      date_epoch: i + 1,
    }));
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: rows,
          meta: { count: 50, limit: 50, offset: 0, total: 200 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-0")).toBeInTheDocument());
    await user.click(screen.getByRole("checkbox", { name: /select all/i }));
    // A button or text containing "select all 200" should appear
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /select all 200/i })).toBeInTheDocument(),
    );
  });

  it('"Select all N": clicking it switches to query-scope q (no ?q= for default tab)', async () => {
    const bulkCalls: Array<{ url: string; body: unknown }> = [];
    const rows = Array.from({ length: 50 }, (_, i) => ({
      uid: `r${i}`,
      host: `srv-${i}`,
      state: "open",
      date_epoch: i + 1,
    }));
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: rows,
          meta: { count: 50, limit: 50, offset: 0, total: 200 },
        }),
      ),
      http.post("/api/v1/record/bulk_state", async ({ request }) => {
        bulkCalls.push({ url: request.url, body: await request.json() });
        return HttpResponse.json({ matched: 200, updated: 200, state: "ack" });
      }),
    );
    const user = userEvent.setup();
    // Navigate to default tab (no ?tab= param). buildQueryParam returns the
    // ACTIVE_ALERTS condition for the default "alerts" tab.
    setup("/web/alerts");
    await waitFor(() => expect(screen.getByText("srv-0")).toBeInTheDocument());
    await user.click(screen.getByRole("checkbox", { name: /select all/i }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /select all 200/i })).toBeInTheDocument(),
    );
    await user.click(screen.getByRole("button", { name: /select all 200/i }));
    await user.click(screen.getByRole("button", { name: /acknowledge \(all 200\)/i }));
    await user.click(screen.getByRole("button", { name: /^acknowledge$/i }));
    await waitFor(() => expect(bulkCalls).toHaveLength(1));
    // The default "alerts" tab encodes the ACTIVE_ALERTS condition, so q IS set
    const url = new URL((bulkCalls[0] as { url: string }).url);
    // q should be the active-alerts tab condition (non-empty), not a uid IN list
    const q = url.searchParams.get("q");
    // q must be non-null (ACTIVE_ALERTS tab always contributes a condition)
    expect(q).not.toBeNull();
    // And the response count (200) surfaces in the toast
    await waitFor(() => {
      const toasts = toastStore.getSnapshot();
      expect(toasts.some((t) => /200 alerts updated/i.test(t.description))).toBe(true);
    });
  });

  it("comment action still loops per-uid (not bulk_state)", async () => {
    const bulkCalls: unknown[] = [];
    const commentCalls: unknown[] = [];
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            { uid: "r1", host: "srv-1", state: "open", date_epoch: 1 },
            { uid: "r2", host: "srv-2", state: "open", date_epoch: 2 },
          ],
          meta: { count: 2, limit: 50, offset: 0, total: 2 },
        }),
      ),
      http.post("/api/v1/record/bulk_state", async ({ request }) => {
        bulkCalls.push(await request.json());
        return HttpResponse.json({ matched: 0, updated: 0, state: "ack" });
      }),
      http.post("/api/v1/comment", async ({ request }) => {
        commentCalls.push(await request.json());
        return HttpResponse.json({ ok: true });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getByRole("checkbox", { name: /select all/i }));
    await user.click(screen.getByRole("button", { name: /comment \(2\)/i }));
    await user.type(screen.getByPlaceholderText(/type your comment/i), "investigating");
    await user.click(screen.getByRole("button", { name: /^comment$/i }));
    await waitFor(() => expect(commentCalls).toHaveLength(2));
    expect(bulkCalls).toHaveLength(0);
  });

  it("bulk action bar: ack button disabled (not hidden) for an all-closed selection", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            { uid: "r1", host: "srv-1", state: "close", date_epoch: 1 },
            { uid: "r2", host: "srv-2", state: "close", date_epoch: 2 },
          ],
          meta: { count: 2, limit: 50, offset: 0, total: 2 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getByRole("checkbox", { name: /select all/i }));
    // For all-closed rows only Re-open is legal — Acknowledge reports 0 of 2
    // and refuses, and Re-open is offered as usual.
    expect(screen.getByRole("button", { name: /acknowledge \(0 of 2\)/i })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
    expect(screen.getByRole("button", { name: /re-open \(2\)/i })).toBeInTheDocument();
  });

  // ── Mixed-state selections: eligibility counts + partial apply ─────────────

  it("mixed selection: Acknowledge shows an eligibility count and applies to the eligible subset", async () => {
    const bulkCalls: Array<{ url: string; body: unknown }> = [];
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            { uid: "r1", host: "srv-1", state: "open", date_epoch: 1 },
            { uid: "r2", host: "srv-2", state: "ack", date_epoch: 2 },
            { uid: "r3", host: "srv-3", state: "close", date_epoch: 3 },
          ],
          meta: { count: 3, limit: 50, offset: 0, total: 3 },
        }),
      ),
      http.post("/api/v1/record/bulk_state", async ({ request }) => {
        bulkCalls.push({ url: request.url, body: await request.json() });
        return HttpResponse.json({ matched: 1, updated: 1, state: "ack" });
      }),
    );
    const user = userEvent.setup();
    // The "All" tab is what mixes lifecycle states in one page; the stub above
    // returns the mixed set regardless of the filter.
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getByRole("checkbox", { name: /select all/i }));

    // Only the open row can be acked; the button says so instead of vanishing.
    const ack = screen.getByRole("button", { name: /acknowledge \(1 of 3\)/i });
    expect(ack).not.toHaveAttribute("aria-disabled");
    await user.click(ack);

    // The confirm dialog states what it will skip.
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("heading", { name: /acknowledge alert/i })).toBeInTheDocument();
    expect(within(dialog).getByText(/2 of the 3 selected alerts will be skipped/i)).toBeVisible();

    await user.click(within(dialog).getByRole("button", { name: /^acknowledge$/i }));
    await waitFor(() => expect(bulkCalls).toHaveLength(1));

    // The request targets ONLY the eligible uid.
    const rawQ = new URL(bulkCalls[0]!.url).searchParams.get("q") ?? "";
    expect(JSON.stringify(decodeConditionQ(rawQ))).toContain("r1");
    expect(JSON.stringify(decodeConditionQ(rawQ))).not.toContain("r2");

    // …and the toast owns up to the rows it left alone.
    await waitFor(() => {
      const toasts = toastStore.getSnapshot();
      expect(toasts.some((t) => /2 skipped/i.test(t.description ?? ""))).toBe(true);
    });
  });

  it("mixed selection: Close is offered for every row that isn't already closed", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            { uid: "r1", host: "srv-1", state: "open", date_epoch: 1 },
            { uid: "r2", host: "srv-2", state: "ack", date_epoch: 2 },
            { uid: "r3", host: "srv-3", state: "close", date_epoch: 3 },
          ],
          meta: { count: 3, limit: 50, offset: 0, total: 3 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getByRole("checkbox", { name: /select all/i }));
    expect(screen.getByRole("button", { name: /close \(2 of 3\)/i })).toBeInTheDocument();
  });

  // ── Plan 34b: timed shelve via ShelveDialog ────────────────────────────────

  it("shelve action on open row opens ShelveDialog and POSTs type=shelve with duration", async () => {
    const calls: unknown[] = [];
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.post("/api/v1/comment", async ({ request }) => {
        calls.push(await request.json());
        return HttpResponse.json({ ok: true });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
    await user.click(screen.getByRole("menuitem", { name: /^shelve(?! permanently)/i }));
    // ShelveDialog should open
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());
    // Submit with default 4h duration
    await user.click(screen.getByRole("button", { name: /^shelve$/i }));
    await waitFor(() => expect(calls.length).toBe(1));
    expect(calls[0]).toMatchObject({ record_uid: "r1", type: "shelve", duration: 14400 });
  });

  it("unshelve on shelved row POSTs type=unshelve to /api/v1/comment", async () => {
    const calls: unknown[] = [];
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "shelved", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.post("/api/v1/comment", async ({ request }) => {
        calls.push(await request.json());
        return HttpResponse.json({ ok: true });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
    await user.click(screen.getByRole("menuitem", { name: /^unshelve\b/i }));
    await waitFor(() => expect(calls.length).toBe(1));
    expect(calls[0]).toMatchObject({ record_uid: "r1", type: "unshelve" });
  });

  // A permanently-shelved row is hidden by `ttl < 0`, not by `state`. Posting
  // an unshelve comment sets state="open" and touches no ttl, so before this
  // fix "Unshelve" on such a row was a no-op the operator could see: the row
  // stayed hidden behind the same `ttl < 0` filter (tabs.ts).
  it("unshelve on a permanently-shelved row (ttl<0) restores a positive ttl", async () => {
    const comments: unknown[] = [];
    const patches: unknown[] = [];
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "open", ttl: -172800, date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.post("/api/v1/comment", async ({ request }) => {
        comments.push(await request.json());
        return HttpResponse.json({ ok: true });
      }),
      http.patch("/api/v1/record/:uid", async ({ request }) => {
        patches.push(await request.json());
        return HttpResponse.json({ ok: true });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
    await user.click(screen.getByRole("menuitem", { name: /^unshelve\b/i }));
    await waitFor(() => expect(patches.length).toBe(1));
    // Magnitude preserved, sign flipped back.
    expect(patches[0]).toMatchObject({ ttl: 172800 });
    // state was never "shelved", so there is no shelve comment to reverse.
    expect(comments).toHaveLength(0);
  });

  it("unshelve on a row that is both shelved and ttl<0 reverses both", async () => {
    const comments: unknown[] = [];
    const patches: unknown[] = [];
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "shelved", ttl: -3600, date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.post("/api/v1/comment", async ({ request }) => {
        comments.push(await request.json());
        return HttpResponse.json({ ok: true });
      }),
      http.patch("/api/v1/record/:uid", async ({ request }) => {
        patches.push(await request.json());
        return HttpResponse.json({ ok: true });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
    await user.click(screen.getByRole("menuitem", { name: /^unshelve\b/i }));
    await waitFor(() => expect(patches.length).toBe(1));
    expect(comments[0]).toMatchObject({ record_uid: "r1", type: "unshelve" });
    expect(patches[0]).toMatchObject({ ttl: 3600 });

    // Undo must reverse BOTH halves, not just the one that is easiest to see.
    // Fire the toast's Undo action directly (the Toaster isn't mounted here).
    const undoToast = await waitFor(() => {
      const t = toastStore.getSnapshot().find((x) => x.action);
      expect(t).toBeTruthy();
      return t!;
    });
    act(() => undoToast.action!.onSelect());
    await waitFor(() => expect(patches.length).toBe(2));
    expect(comments[1]).toMatchObject({ record_uid: "r1", type: "shelve" });
    expect(patches[1]).toMatchObject({ ttl: -3600 });
  });
});

// ── Plan 28b: console branding consumption ────────────────────────────────

/** MSW response body for GET /api/v1/config */
function makeConfigResponse(overrides: Record<string, unknown> = {}) {
  return {
    data: {
      columns: ["date_epoch", "severity", "state", "host", "message"],
      default_filter: "",
      sort_by: "-date_epoch",
      refresh_interval: 5,
      severity_ranks: {},
      severity_order: [],
      logo: "",
      title: "",
      audio: "",
      clipboard_template: "",
      ...overrides,
    },
  };
}

describe("AlertsPage — Plan 28b: clipboard template", () => {
  afterEach(() => {
    toastStore.clear();
    vi.restoreAllMocks();
  });

  it("copy-json with empty template copies JSON stringify output", async () => {
    const written: string[] = [];
    vi.spyOn(navigator.clipboard, "writeText").mockImplementation((text: string) => {
      written.push(text);
      return Promise.resolve();
    });
    mswServer.use(
      http.get("/api/v1/config", () => HttpResponse.json(makeConfigResponse())),
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "db01", message: "disk full", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("db01")).toBeInTheDocument());
    const row = screen.getByText("db01").closest("tr")!;
    await user.pointer({ keys: "[MouseRight]", target: row });
    await waitFor(() =>
      expect(screen.getByRole("menu", { name: /row context menu/i })).toBeInTheDocument(),
    );
    // With empty clipboard_template, label is "Copy as JSON"
    await user.click(screen.getByRole("menuitem", { name: /copy as json/i }));
    await waitFor(() => expect(written.length).toBeGreaterThan(0));
    // Should be JSON — parseable and contain the host field
    const parsed = JSON.parse(written[0]!) as Record<string, unknown>;
    expect(parsed.host).toBe("db01");
  });

  it("copy action with clipboard_template substitutes template fields", async () => {
    const written: string[] = [];
    vi.spyOn(navigator.clipboard, "writeText").mockImplementation((text: string) => {
      written.push(text);
      return Promise.resolve();
    });
    mswServer.use(
      http.get("/api/v1/config", () =>
        HttpResponse.json(makeConfigResponse({ clipboard_template: "{{host}} — {{message}}" })),
      ),
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "db01", message: "disk full", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("db01")).toBeInTheDocument());
    const row = screen.getByText("db01").closest("tr")!;
    await user.pointer({ keys: "[MouseRight]", target: row });
    await waitFor(() =>
      expect(screen.getByRole("menu", { name: /row context menu/i })).toBeInTheDocument(),
    );
    // With a template set, label becomes "Copy" (not "Copy as JSON")
    await user.click(screen.getByRole("menuitem", { name: /^copy$/i }));
    await waitFor(() => expect(written.length).toBeGreaterThan(0));
    expect(written[0]).toBe("db01 — disk full");
  });

  it("'Snooze this alert' (kebab) navigates to /web/snoozes prefilled with host+message and a 1h window", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "db01", message: "disk full", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    const router = setup("/web/alerts", { withSnoozesStub: true });
    await waitFor(() => expect(screen.getByText("db01")).toBeInTheDocument());
    await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
    await user.click(screen.getByRole("menuitem", { name: /snooze this alert/i }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/web/snoozes"));
    const params = new URLSearchParams(router.state.location.searchStr);
    const decoded = decodeConditionQ(params.get("prefillCond") ?? "");
    expect(decoded).toMatchObject({
      type: "AND",
      args: [
        { type: "EQUALS", field: "host", value: "db01" },
        { type: "EQUALS", field: "message", value: "disk full" },
      ],
    });
    expect(params.get("prefillName")).toMatch(/db01/);
    expect(params.get("prefillComment")).toMatch(/r1/);
    expect(params.get("prefillSeconds")).toBe("3600");
  });

  it("'Snooze this alert' is also offered from the right-click context menu", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "db01", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup("/web/alerts", { withSnoozesStub: true });
    await waitFor(() => expect(screen.getByText("db01")).toBeInTheDocument());
    const row = screen.getByText("db01").closest("tr")!;
    await user.pointer({ keys: "[MouseRight]", target: row });
    await waitFor(() =>
      expect(screen.getByRole("menu", { name: /row context menu/i })).toBeInTheDocument(),
    );
    expect(screen.getByRole("menuitem", { name: /snooze this alert/i })).toBeInTheDocument();
  });

  it("bulk-snoozing 5 or fewer selected rows navigates straight to /web/snoozes with an OR'd condition", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            { uid: "r1", host: "db01", message: "disk full", state: "open", date_epoch: 1 },
            { uid: "r2", host: "db02", message: "disk full", state: "open", date_epoch: 2 },
            { uid: "r3", host: "db03", message: "disk full", state: "open", date_epoch: 3 },
          ],
          meta: { count: 3, limit: 50, offset: 0, total: 3 },
        }),
      ),
    );
    const user = userEvent.setup();
    const router = setup("/web/alerts", { withSnoozesStub: true });
    await waitFor(() => expect(screen.getByText("db01")).toBeInTheDocument());
    await user.click(screen.getByRole("checkbox", { name: /select all/i }));
    await user.click(screen.getByRole("button", { name: /^snooze \(3\)$/i }));
    // No warning dialog at or below the threshold — it should navigate immediately.
    expect(screen.queryByRole("dialog")).toBeNull();
    await waitFor(() => expect(router.state.location.pathname).toBe("/web/snoozes"));
    const params = new URLSearchParams(router.state.location.searchStr);
    const decoded = decodeConditionQ(params.get("prefillCond") ?? "");
    expect(decoded).toMatchObject({
      type: "OR",
      args: [
        { type: "AND", args: [{ field: "host", value: "db01" }, { field: "message" }] },
        { type: "AND", args: [{ field: "host", value: "db02" }, { field: "message" }] },
        { type: "AND", args: [{ field: "host", value: "db03" }, { field: "message" }] },
      ],
    });
    expect(params.get("prefillName")).toMatch(/3 alerts/);
  });

  it("bulk-snoozing more than 5 selected rows warns with a compact preview before navigating", async () => {
    const rows = Array.from({ length: 6 }, (_, i) => ({
      uid: `r${i + 1}`,
      host: `db0${i + 1}`,
      message: "disk full",
      state: "open",
      date_epoch: i + 1,
    }));
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: rows,
          meta: { count: 6, limit: 50, offset: 0, total: 6 },
        }),
      ),
    );
    const user = userEvent.setup();
    const router = setup("/web/alerts", { withSnoozesStub: true });
    await waitFor(() => expect(screen.getByText("db01")).toBeInTheDocument());
    await user.click(screen.getByRole("checkbox", { name: /select all/i }));
    await user.click(screen.getByRole("button", { name: /^snooze \(6\)$/i }));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/snooze 6 alerts\?/i)).toBeInTheDocument();
    // Preview is capped at 5 lines with a "+1 more" tail.
    expect(within(dialog).getByText(/db01 — disk full/)).toBeInTheDocument();
    expect(within(dialog).getByText(/db05 — disk full/)).toBeInTheDocument();
    expect(within(dialog).queryByText(/db06 — disk full/)).toBeNull();
    expect(within(dialog).getByText(/\+1 more/)).toBeInTheDocument();

    // Cancel: no navigation.
    await user.click(within(dialog).getByRole("button", { name: /cancel/i }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(router.state.location.pathname).toBe("/web/alerts");

    // Continue: navigates with all 6 rows OR'd together.
    await user.click(screen.getByRole("button", { name: /^snooze \(6\)$/i }));
    const dialog2 = await screen.findByRole("dialog");
    await user.click(within(dialog2).getByRole("button", { name: /continue/i }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/web/snoozes"));
    const params = new URLSearchParams(router.state.location.searchStr);
    const decoded = decodeConditionQ(params.get("prefillCond") ?? "");
    expect(decoded?.type).toBe("OR");
    expect(decoded && "args" in decoded ? decoded.args.length : -1).toBe(6);
    expect(params.get("prefillName")).toMatch(/6 alerts/);
  });

  it("shows a plain 'Snooze' badge (no count) next to the SearchBar's clear button once the typed filter matches alerts, and it prefills the snooze editor with that filter", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "db01", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 3 },
        }),
      ),
      http.post("/api/v1/condition/parse", () =>
        HttpResponse.json({ condition: { op: "EQUALS", field: "host", value: "db01" } }),
      ),
    );
    const user = userEvent.setup();
    const router = setup("/web/alerts", { withSnoozesStub: true });
    await waitFor(() => expect(screen.getByText("db01")).toBeInTheDocument());
    // No badge before any search text is entered.
    expect(screen.queryByText(/^snooze$/i)).toBeNull();

    await user.type(screen.getByRole("textbox", { name: /search/i }), "host = db01");
    // No count in the label — the visible total reflects the active tab/env
    // filter too, so it can't be presented as a promise of what the search
    // condition alone will match once saved without that tab preset.
    const badge = await screen.findByText(/^snooze$/i);

    await user.click(badge);
    await waitFor(() => expect(router.state.location.pathname).toBe("/web/snoozes"));
    const params = new URLSearchParams(router.state.location.searchStr);
    const decoded = decodeConditionQ(params.get("prefillCond") ?? "");
    // The frontend's Condition shape (type/args) that ConditionEditor and
    // encodeConditionQ expect — not the backend's own op/children wire shape
    // the mocked /condition/parse response above returns (that response only
    // gates whether the badge shows; the prefilled condition is parsed from
    // the raw search text via the same DSL parser ConditionEditor uses).
    expect(decoded).toMatchObject({ type: "EQUALS", field: "host", value: "db01" });
    expect(params.get("prefillName")).toMatch(/search/i);
    expect(params.get("prefillComment")).toMatch(/host = db01/);
    // No stale/misleading count baked into the comment either.
    expect(params.get("prefillComment")).not.toMatch(/\d+ alert/);
  });

  it("selecting every row of a single-page, non-empty search skips the bulk-snooze warning and prefills with the search filter itself", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            { uid: "r1", host: "db01", state: "open", date_epoch: 1 },
            { uid: "r2", host: "db02", state: "open", date_epoch: 2 },
          ],
          meta: { count: 2, limit: 50, offset: 0, total: 2 },
        }),
      ),
      http.post("/api/v1/condition/parse", () =>
        HttpResponse.json({ condition: { op: "EQUALS", field: "host", value: "db0" } }),
      ),
    );
    const user = userEvent.setup();
    const router = setup("/web/alerts", { withSnoozesStub: true });
    await waitFor(() => expect(screen.getByText("db01")).toBeInTheDocument());
    await user.type(screen.getByRole("textbox", { name: /search/i }), "host contains db0");
    // Wait for the parse to resolve (the badge is a convenient signal that
    // searchCondition has landed) before selecting rows.
    await screen.findByText(/^snooze$/i);

    await user.click(screen.getByRole("checkbox", { name: /select all/i }));
    await user.click(screen.getByRole("button", { name: /^snooze \(2\)$/i }));

    // No warning dialog — every matching row (2 of 2) is already selected.
    expect(screen.queryByRole("dialog")).toBeNull();
    await waitFor(() => expect(router.state.location.pathname).toBe("/web/snoozes"));
    const params = new URLSearchParams(router.state.location.searchStr);
    const decoded = decodeConditionQ(params.get("prefillCond") ?? "");
    // The search condition itself (frontend Condition shape), not an OR of
    // the two rows' host/message.
    expect(decoded).toMatchObject({ type: "CONTAINS", field: "host", value: "db0" });
    expect(params.get("prefillName")).toMatch(/search/i);
  });
});

describe("AlertsPage — Plan 28b: default_filter seeding", () => {
  afterEach(() => {
    toastStore.clear();
  });

  it("pre-fills SearchBar from config.default_filter when no ?search= URL param", async () => {
    mswServer.use(
      http.get("/api/v1/config", () =>
        HttpResponse.json(makeConfigResponse({ default_filter: "severity = critical" })),
      ),
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
    );
    setup("/web/alerts");
    const input = await screen.findByRole("textbox", { name: /search/i });
    await waitFor(() => expect((input as HTMLInputElement).value).toBe("severity = critical"));
  });

  it("URL ?search= wins over config.default_filter", async () => {
    mswServer.use(
      http.get("/api/v1/config", () =>
        HttpResponse.json(makeConfigResponse({ default_filter: "severity = critical" })),
      ),
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
    );
    setup("/web/alerts?search=host%20%3D%20db01");
    const input = await screen.findByRole("textbox", { name: /search/i });
    // URL takes precedence — should be "host = db01", not "severity = critical"
    await waitFor(() => expect((input as HTMLInputElement).value).toBe("host = db01"));
  });
});

describe("AlertsPage — Plan 28b: audio cue", () => {
  afterEach(() => {
    toastStore.clear();
    vi.restoreAllMocks();
  });

  it("does not call new Audio when config.audio is empty", async () => {
    const audioMock = vi.fn().mockReturnValue({ play: vi.fn().mockResolvedValue(undefined) });
    vi.stubGlobal("Audio", audioMock);

    mswServer.use(
      http.get("/api/v1/config", () => HttpResponse.json(makeConfigResponse({ audio: "" }))),
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 5 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    expect(audioMock).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });

  it("does not play on first load even when total > 0 (prevTotalRef starts at -1)", async () => {
    const playMock = vi.fn().mockResolvedValue(undefined);
    const audioMock = vi.fn().mockReturnValue({ play: playMock });
    vi.stubGlobal("Audio", audioMock);

    mswServer.use(
      http.get("/api/v1/config", () =>
        HttpResponse.json(makeConfigResponse({ audio: "https://example.com/alert.wav" })),
      ),
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 5 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    // First load: prevTotalRef.current was -1, so no play should have occurred
    expect(playMock).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });
});

// ── Close is not danger-weighted; the four silencing verbs explain themselves ──

describe("AlertsPage — silencing verbs", () => {
  function oneOpenRow() {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
  }

  it("the kebab groups its items under Change state / Engage / Quiet it down", async () => {
    oneOpenRow();
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
    expect(screen.getByText("Change state")).toBeInTheDocument();
    expect(screen.getByText("Engage")).toBeInTheDocument();
    expect(screen.getByText("Quiet it down")).toBeInTheDocument();
  });

  it("the kebab explains Close, Snooze, Shelve and Shelve permanently — and nothing else", async () => {
    oneOpenRow();
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
    expect(screen.getByText(SILENCE_DESCRIPTIONS.close)).toBeInTheDocument();
    expect(screen.getByText(SILENCE_DESCRIPTIONS.snooze)).toBeInTheDocument();
    expect(screen.getByText(SILENCE_DESCRIPTIONS.shelve)).toBeInTheDocument();
    expect(screen.getByText(SILENCE_DESCRIPTIONS.shelveForever)).toBeInTheDocument();
    // Acknowledge / Re-escalate / Comment say what they do; a description on
    // every row would cost the menu the scannability the groups just bought.
    const acknowledge = screen.getByRole("menuitem", { name: /^acknowledge$/i });
    expect(acknowledge).toBeInTheDocument();
  });

  it("Snooze stays in the kebab for a closed row, the shelve verbs do not", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "close", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
    expect(screen.getByRole("menuitem", { name: /^snooze this alert/i })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: /^shelve/i })).toBeNull();
  });

  it("the bulk bar makes Acknowledge the primary button and Close a plain secondary", async () => {
    oneOpenRow();
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getAllByRole("checkbox")[1]!);
    const ack = await screen.findByRole("button", { name: /acknowledge \(1\)/i });
    const close = screen.getByRole("button", { name: /close \(1\)/i });
    // Red is reserved for Delete: Close is reversible (Re-open / Undo), so it
    // must not carry the danger class, and Acknowledge takes the one filled
    // button on the bar.
    expect(ack.className).toMatch(/primary/);
    expect(close.className).not.toMatch(/danger/);
    expect(close.className).toMatch(/secondary/);
  });

  it("the detail drawer labels the alert verb 'Close alert', distinct from the panel's own ✕", async () => {
    oneOpenRow();
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getAllByRole("button", { name: /view details/i })[0]!);
    const drawerClose = await screen.findByRole("button", { name: /^close panel$/i });
    expect(drawerClose).toBeInTheDocument();
    const closeAlert = screen.getByRole("button", { name: /^close alert$/i });
    expect(closeAlert.className).not.toMatch(/danger/);
    expect(screen.getByRole("button", { name: /^acknowledge$/i }).className).toMatch(/primary/);
  });
});

// ── Screen-reader parity for the 30s poll ─────────────────────────────────

describe("AlertsPage — refresh announcements", () => {
  const polite = () => screen.getByTestId("live-polite");

  /** The live region is written one coalesce window + one repaint after the
   *  data lands, so "nothing was announced" needs a wait long enough to have
   *  caught an announcement had there been one. */
  const settleAnnouncer = () => act(() => new Promise((r) => setTimeout(r, 500)));

  function rowsWithTotals(totals: number[]) {
    let call = 0;
    mswServer.use(
      http.get("/api/v1/record", () => {
        const total = totals[Math.min(call++, totals.length - 1)]!;
        return HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total },
        });
      }),
    );
  }

  it("announces a background refresh that changed the total", async () => {
    rowsWithTotals([5, 7]);
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: /refresh alerts/i }));
    await waitFor(() => expect(polite()).toHaveTextContent("Alerts refreshed. 7 alerts, 2 new."));
  });

  it("says 'fewer' when the queue shrank, and agrees with a count of one", async () => {
    rowsWithTotals([3, 1]);
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: /refresh alerts/i }));
    await waitFor(() => expect(polite()).toHaveTextContent("Alerts refreshed. 1 alert, 2 fewer."));
  });

  it("says nothing on first load", async () => {
    rowsWithTotals([5]);
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await settleAnnouncer();
    expect(polite().textContent).toBe("");
  });

  it("says nothing when the operator's own filter change moved the total", async () => {
    // The default "alerts" tab sends a ?q=; the "All" tab sends none. Answer
    // with a different total per question so the count really does move.
    mswServer.use(
      http.get("/api/v1/record", ({ request }) => {
        const total = new URL(request.url).searchParams.get("q") ? 5 : 9;
        return HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", state: "open", date_epoch: 1 }],
          meta: { count: 1, limit: 50, offset: 0, total },
        });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getByRole("tab", { name: /^all$/i }));
    await waitFor(() => expect(screen.getByText("9 alerts")).toBeInTheDocument());
    await settleAnnouncer();
    expect(polite().textContent).toBe("");
  });

  it("announces the auto-refresh toggle", async () => {
    rowsWithTotals([5]);
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await user.click(screen.getByRole("switch", { name: /auto refresh/i }));
    await waitFor(() => expect(polite()).toHaveTextContent("Auto-refresh off"));
  });
});

describe("AlertsPage inspector guards", () => {
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

  afterEach(() => {
    toastStore.clear();
    authStore.getState().logout({ revoke: false });
  });

  function twoRows() {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            { uid: "r1", host: "srv-1", severity: "critical", state: "open", date_epoch: 1 },
            { uid: "r2", host: "srv-2", severity: "critical", state: "open", date_epoch: 2 },
          ],
          meta: { count: 2, limit: 50, offset: 0, total: 2 },
        }),
      ),
    );
  }

  it("drops a dangling ?analysis= that has no ?record= to ride with", async () => {
    // Left behind by a closed inspector, it would send the next plain row
    // click to the Analysis tab instead of the Timeline.
    twoRows();
    const router = setup("/web/alerts?analysis=1");
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());
    await waitFor(() =>
      expect((router.state.location.search as { analysis?: unknown }).analysis).toBeUndefined(),
    );
  });

  it("asks before prev/next retargets the drawer away from an open analysis editor", async () => {
    const user = userEvent.setup();
    loginWithPerms(["ro_record", "rw_record", "rw_protected"]);
    twoRows();
    mswServer.use(
      http.get("/api/v1/record/:uid/agentic", ({ params }) =>
        HttpResponse.json({
          uid: params["uid"] as string,
          agentic: {
            root_cause: { summary: "journald filled /var", confidence: "high" },
            remediation_plan: { steps: [{ action: "Vacuum the journal", risk: "low" }] },
          },
        }),
      ),
    );
    const router = setup("/web/alerts?record=r1&analysis=1");

    const drawer = await screen.findByRole("dialog");
    await user.click(await within(drawer).findByRole("button", { name: "Edit" }));
    expect(await screen.findByLabelText("Summary")).toBeInTheDocument();

    // J / the chevron retarget the drawer at r2 while the editor still holds a
    // draft written against r1 — and the editor PUTs to whatever uid is
    // current at submit time.
    await user.click(within(drawer).getByRole("button", { name: /Next row/ }));
    const confirm = await screen.findByRole("dialog", { name: "Discard this analysis draft?" });
    await user.click(within(confirm).getByRole("button", { name: "Keep editing" }));

    expect((router.state.location.search as { record?: string }).record).toBe("r1");
    expect(screen.getByLabelText("Summary")).toBeInTheDocument();

    await user.click(within(drawer).getByRole("button", { name: /Next row/ }));
    await user.click(
      within(await screen.findByRole("dialog", { name: "Discard this analysis draft?" })).getByRole(
        "button",
        { name: "Discard draft" },
      ),
    );
    await waitFor(() =>
      expect((router.state.location.search as { record?: string }).record).toBe("r2"),
    );
  });
});
