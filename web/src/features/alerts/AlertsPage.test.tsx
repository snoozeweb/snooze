import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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
});
