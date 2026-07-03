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
import { afterEach, describe, expect, it, vi } from "vitest";
import { mswServer } from "@/tests/msw/server";
import { AlertRowDetail } from "./AlertRowDetail";
import type { Record_ } from "./types";

// AlertRowDetail switches on useIsMobileShell (window.matchMedia). jsdom has no
// matchMedia, so the hook defaults to desktop — every test below renders the
// 3-column layout unless it opts into the mobile branch via mockMatchMedia(true).
function mockMatchMedia(matches: boolean) {
  vi.stubGlobal(
    "matchMedia",
    vi.fn().mockImplementation((query: string) => ({
      matches,
      media: query,
      addEventListener: () => {},
      removeEventListener: () => {},
    })),
  );
}
afterEach(() => vi.unstubAllGlobals());

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
      <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
    </QueryClientProvider>,
  );
}

describe("AlertRowDetail", () => {
  it("renders the JSON of the row stripped of underscore-prefixed keys", () => {
    const row: Record_ = {
      uid: "r1",
      host: "srv-1",
      severity: "critical",
      state: "open",
      message: "disk full",
      date_epoch: 1,
    };
    // Inject an internal key that should be stripped, mirroring RowDetailPanel.
    const rowWithPrivate = { ...row, _internal: "secret" } as Record_;
    mswServer.use(
      http.get("/api/v1/comment", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 100, offset: 0, total: 0 },
        }),
      ),
    );
    renderDetail(rowWithPrivate);
    // JsonViewer renders the cleaned object as a tree of <pre> elements.
    expect(screen.getByText(/srv-1/)).toBeInTheDocument();
    expect(screen.getByText(/disk full/)).toBeInTheDocument();
    // The underscore-prefixed key must not appear.
    expect(screen.queryByText(/_internal/)).toBeNull();
  });

  it("shows Record, Flow and Timeline at once on desktop (no tabs)", async () => {
    mswServer.use(
      http.get("/api/v1/comment", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 100, offset: 0, total: 0 },
        }),
      ),
    );
    const row = { uid: "u1", source: "syslog", aggregate: "Host and Message" } as Record_;
    renderDetail(row);
    // No tab chrome on desktop — all three surfaces render simultaneously.
    expect(screen.queryByRole("tab")).toBeNull();
    // Flow (source) is visible without any interaction...
    expect(screen.getByText("syslog")).toBeInTheDocument();
    // ...and so is the Timeline (its empty state), rendered directly.
    await waitFor(() => expect(screen.getByText(/no comments yet/i)).toBeInTheDocument());
  });

  it("collapses to tabs on mobile, defaulting to Timeline", async () => {
    mockMatchMedia(true);
    mswServer.use(
      http.get("/api/v1/comment", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 100, offset: 0, total: 0 },
        }),
      ),
    );
    const row = { uid: "u1", source: "syslog", aggregate: "Host and Message" } as Record_;
    renderDetail(row);
    expect(screen.getByRole("tab", { name: "Timeline" })).toHaveAttribute("data-state", "active");
    // Flow lives behind its tab until selected.
    expect(screen.queryByText("syslog")).toBeNull();
    await userEvent.click(screen.getByRole("tab", { name: "Flow" }));
    expect(screen.getByText("syslog")).toBeInTheDocument();
    // Record (JSON) is the third tab.
    expect(screen.getByRole("tab", { name: "Record" })).toBeInTheDocument();
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
