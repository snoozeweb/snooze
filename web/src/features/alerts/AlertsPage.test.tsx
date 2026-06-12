import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it } from "vitest";
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
import { AlertsPage } from "./AlertsPage";

function setup(pathname = "/web/alerts") {
  const root = createRootRoute({ component: () => <Outlet /> });
  const alerts = createRoute({
    getParentRoute: () => root,
    path: "/web/alerts",
    component: AlertsPage,
  });
  const tree = root.addChildren([alerts]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: [pathname] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        {/* router is locally constructed; cast needed for the registered-router type mismatch */}
        <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
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

  it("offers an Acknowledge action on open rows that POSTs to /comment via dialog", async () => {
    const calls: unknown[] = [];
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", severity: "info", state: "open", date_epoch: 1 }],
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
    await user.click(screen.getByRole("menuitem", { name: /acknowledge/i }));
    // Dialog should appear; confirm it
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: /^acknowledge$/i }));
    await waitFor(() => expect(calls.length).toBe(1));
    expect(calls[0]).toMatchObject({ record_uid: "r1", type: "ack" });
  });

  it("expanding a row via the chevron renders the JSON + CommentTimeline", async () => {
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
    // No drawer should mount on the bare list.
    expect(screen.queryByRole("dialog")).toBeNull();
    // Toggle the inline expansion.
    await user.click(screen.getByRole("button", { name: /^Expand row /i }));
    // The expansion panel surfaces the CommentTimeline empty state and the
    // alert uid in the JsonViewer.
    await waitFor(() => expect(screen.getByText(/no comments yet/i)).toBeInTheDocument());
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("bulk acknowledge: posts one comment per selected row, then clears selection", async () => {
    const calls: unknown[] = [];
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
      http.post("/api/v1/comment", async ({ request }) => {
        calls.push(await request.json());
        return HttpResponse.json({ ok: true });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("srv-1")).toBeInTheDocument());

    await user.click(screen.getByRole("checkbox", { name: /select all/i }));
    await user.click(screen.getByRole("button", { name: /acknowledge \(2\)/i }));
    await user.click(screen.getByRole("button", { name: /^acknowledge$/i }));

    await waitFor(() => expect(calls).toHaveLength(2));
    const types = (calls as Array<{ type: string }>).map((c) => c.type);
    expect(types.every((t) => t === "ack")).toBe(true);

    // Bulk ack raises a count-bearing undo toast ("N alerts updated") that
    // still carries the Undo affordance (re-opens every succeeded uid).
    await waitFor(() => {
      const toasts = toastStore.getSnapshot();
      expect(toasts.some((t) => /2 alerts updated/i.test(t.description) && t.action)).toBe(true);
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
    expect(screen.getByRole("button", { name: /^close$/i })).toBeInTheDocument();
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

  it("keyboard 'a' on a focused open row fires an inline ack", async () => {
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

    // Focus the grid, move to the first row (ArrowDown), then press 'a'.
    const grid = screen.getByRole("grid");
    grid.focus();
    await user.keyboard("{ArrowDown}a");

    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toMatchObject({ record_uid: "r1", type: "ack" });
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
});
