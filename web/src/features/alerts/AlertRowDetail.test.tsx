import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { mswServer } from "@/tests/msw/server";
import { AlertRowDetail } from "./AlertRowDetail";
import type { Record_ } from "./types";

// Empty comment list so CommentTimeline (the default Timeline tab) resolves to
// its empty state in every test.
function stubComments() {
  mswServer.use(
    http.get("/api/v1/comment", () =>
      HttpResponse.json({ data: [], meta: { count: 0, limit: 100, offset: 0, total: 0 } }),
    ),
  );
}

// AlertRowDetail's Flow tab embeds AlertFlowChart, whose entities are TanStack
// <Link>s — so the detail needs a RouterProvider ancestor (app-wide in
// production via app/router.tsx). Stand up a minimal memory router whose home
// route hosts the detail and stubs the deep-link targets so the links resolve.
function renderDetail(row: Record_) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const root = createRootRoute({ component: () => <Outlet /> });
  const home = createRoute({
    getParentRoute: () => root,
    path: "/",
    component: () => <AlertRowDetail row={row} />,
  });
  const stub = (path: string) =>
    createRoute({ getParentRoute: () => root, path, component: () => <div>{path}</div> });
  const tree = root.addChildren([
    home,
    stub("/web/rules"),
    stub("/web/snoozes"),
    stub("/web/notifications"),
  ]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: ["/"] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        {/* The summary header's TimeCell renders a Tooltip, which needs a
            provider ancestor (app-wide in production). */}
        <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("AlertRowDetail", () => {
  it("renders the summary header (severity, state, source, message, time) without repeating host", () => {
    stubComments();
    const row: Record_ = {
      uid: "r1",
      host: "srv-1",
      severity: "critical",
      state: "open",
      message: "disk full",
      source: "prom",
      date_epoch: 1,
    };
    renderDetail(row);
    // Severity + state badges. Severity renders title-cased ("Critical") for
    // display, with the raw "critical" wire token kept as the badge's title.
    expect(screen.getByText("Critical")).toBeInTheDocument();
    expect(screen.getByText("Open")).toBeInTheDocument();
    // Source chip.
    expect(screen.getByText("prom")).toBeInTheDocument();
    // Message (selectable) is shown directly in the header.
    expect(screen.getByText("disk full")).toBeInTheDocument();
    // Host is the inspector title, so it is NOT repeated in the detail body.
    expect(screen.queryByText("srv-1")).toBeNull();
  });

  it("shows an escalation badge only once the alert has been re-escalated", () => {
    stubComments();
    const base: Record_ = {
      uid: "r1",
      host: "srv-1",
      severity: "critical",
      state: "esc",
      message: "disk full",
      date_epoch: 1,
    };

    // A first-delivery alert carries no extra chrome — just the state chip,
    // which for state=esc already reads "Re-escalated" (the canonical noun
    // shared with the tab/tile/legend).
    const { unmount } = renderDetail(base);
    expect(screen.getAllByText(/Re-escalated/)).toHaveLength(1);
    unmount();

    // Once escalation_count is set, a second badge appears with the count
    // and reason. It deliberately shares the "Re-escalated" headline with
    // the state chip (one word per lifecycle fact) — distinguish by the
    // full "x3 (timeout)" text and by there now being two matches.
    renderDetail({ ...base, escalation_count: 3, escalation_reason: "timeout" });
    expect(screen.getByText("Re-escalated x3 (timeout)")).toBeInTheDocument();
    expect(screen.getAllByText(/Re-escalated/)).toHaveLength(2);
  });

  it("attributes a manual escalation to the operator who made it", () => {
    stubComments();
    renderDetail({
      uid: "r1",
      host: "srv-1",
      severity: "critical",
      state: "esc",
      date_epoch: 1,
      escalation_count: 1,
      escalation_reason: "manual",
      escalation_actor: "alice",
    });
    expect(screen.getByText("Re-escalated by alice")).toBeInTheDocument();
  });

  it("shows Timeline / Flow / Record tabs with Timeline active by default", async () => {
    stubComments();
    const row = { uid: "u1", source: "syslog", aggregate: "Host and Message" } as Record_;
    renderDetail(row);
    expect(screen.getByRole("tab", { name: "Timeline" })).toHaveAttribute("data-state", "active");
    expect(screen.getByRole("tab", { name: "Flow" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Record" })).toBeInTheDocument();
    // Timeline content (its empty state) is visible without interaction.
    await waitFor(() => expect(screen.getByText(/no comments yet/i)).toBeInTheDocument());
  });

  it("reveals the Flow chart and the Record JSON when their tabs are selected", async () => {
    stubComments();
    const row = {
      uid: "u1",
      source: "syslog",
      aggregate: "Host and Message",
      _internal: "secret",
    } as Record_;
    renderDetail(row);
    const user = userEvent.setup();
    // Flow lives behind its tab until selected — the aggregate value is
    // Flow-only (the Aggregate node), unlike source which also sits in the chip.
    expect(screen.queryByText("Host and Message")).toBeNull();
    await user.click(screen.getByRole("tab", { name: "Flow" }));
    expect(screen.getByText("Host and Message")).toBeInTheDocument();
    // Record tab renders the row JSON stripped of underscore-prefixed keys. The
    // uid only appears in this JSON tree (not the summary header), so it's an
    // unambiguous marker that the Record surface is showing.
    await user.click(screen.getByRole("tab", { name: "Record" }));
    expect(screen.getByText(/u1/)).toBeInTheDocument();
    expect(screen.queryByText(/_internal/)).toBeNull();
  });

  it("renders a CommentTimeline scoped to the row's uid", async () => {
    mswServer.use(
      http.get("/api/v1/comment", ({ request }) => {
        const url = new URL(request.url);
        // CommentTimeline filters by record_uid via the resource list query.
        // We just need to return an empty list so the empty state shows.
        // Verify the request is for this record.
        expect(url.searchParams.get("q")).not.toBeNull();
        return HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 100, offset: 0, total: 0 },
        });
      }),
    );
    const row: Record_ = { uid: "r1", host: "srv-1", date_epoch: 1 };
    renderDetail(row);
    await waitFor(() => expect(screen.getByText(/no comments yet/i)).toBeInTheDocument());
  });
});
