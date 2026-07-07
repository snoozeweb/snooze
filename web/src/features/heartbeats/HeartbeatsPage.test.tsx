import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { beforeAll, describe, expect, it } from "vitest";
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
import { HeartbeatsPage } from "./HeartbeatsPage";
import type { Heartbeat } from "./types";

beforeAll(() => {
  if (typeof window !== "undefined" && !window.ResizeObserver) {
    window.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

const FIXTURE_OK: Heartbeat = {
  uid: "uid-1",
  name: "hb-ok",
  interval: 60,
  status: "ok",
  last_seen: new Date(Date.now() - 5 * 60 * 1000).toISOString(),
};
const FIXTURE_OVERDUE: Heartbeat = {
  uid: "uid-2",
  name: "hb-overdue",
  interval: 60,
  status: "overdue",
};

function makeListResponse(items: Heartbeat[]) {
  return {
    data: items,
    meta: { count: items.length, limit: 50, offset: 0, total: items.length },
  };
}

function setup(pathname = "/web/heartbeats") {
  const root = createRootRoute({ component: () => <Outlet /> });
  const heartbeats = createRoute({
    getParentRoute: () => root,
    path: "/web/heartbeats",
    component: HeartbeatsPage,
  });
  const tree = root.addChildren([heartbeats]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: [pathname] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider delay={0}>
        <ToastProvider>
          <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
          <Toaster />
        </ToastProvider>
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("HeartbeatsPage", () => {
  it("renders_rows_with_status_badges", async () => {
    mswServer.use(
      http.get("/api/v1/heartbeat", () =>
        HttpResponse.json(makeListResponse([FIXTURE_OK, FIXTURE_OVERDUE])),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("hb-ok")).toBeInTheDocument());
    expect(screen.getByText("hb-overdue")).toBeInTheDocument();
    // Status badges
    expect(screen.getByText("ok")).toBeInTheDocument();
    expect(screen.getByText("overdue")).toBeInTheDocument();
  });

  it("new_button_opens_editor", async () => {
    mswServer.use(
      http.get("/api/v1/heartbeat", () => HttpResponse.json(makeListResponse([FIXTURE_OK]))),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("hb-ok")).toBeInTheDocument());
    const newBtn = screen.getByRole("button", { name: /new$/i });
    await user.click(newBtn);
    // Drawer title
    expect(await screen.findByText("New heartbeat")).toBeInTheDocument();
  });

  it("status_filter_all_renders_both_rows", async () => {
    mswServer.use(
      http.get("/api/v1/heartbeat", () =>
        HttpResponse.json(makeListResponse([FIXTURE_OK, FIXTURE_OVERDUE])),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("hb-ok")).toBeInTheDocument());
    expect(screen.getByText("hb-overdue")).toBeInTheDocument();
  });

  it("status_filter_overdue_calls_api_with_status", async () => {
    let capturedUrl: string | undefined;
    mswServer.use(
      http.get("/api/v1/heartbeat", ({ request }) => {
        capturedUrl = request.url;
        const url = new URL(request.url);
        const statusParam = url.searchParams.get("status");
        const items = statusParam === "overdue" ? [FIXTURE_OVERDUE] : [FIXTURE_OK, FIXTURE_OVERDUE];
        return HttpResponse.json(makeListResponse(items));
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("hb-ok")).toBeInTheDocument());

    // Click the Overdue filter tab
    await user.click(screen.getByRole("tab", { name: /overdue/i }));

    await waitFor(() => {
      expect(capturedUrl).toContain("status=overdue");
    });
    // After filtering, only overdue row is visible
    await waitFor(() => expect(screen.getByText("hb-overdue")).toBeInTheDocument());
  });

  it("status_filter_all_clears_status_param", async () => {
    // Regression: clicking "All" must clear the status query. The old handler
    // set no `status` key, so the merge kept the previous filter and "All"
    // silently did nothing.
    const capturedStatuses: (string | null)[] = [];
    mswServer.use(
      http.get("/api/v1/heartbeat", ({ request }) => {
        const statusParam = new URL(request.url).searchParams.get("status");
        capturedStatuses.push(statusParam);
        const items = statusParam === "overdue" ? [FIXTURE_OVERDUE] : [FIXTURE_OK, FIXTURE_OVERDUE];
        return HttpResponse.json(makeListResponse(items));
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("hb-ok")).toBeInTheDocument());

    // Filter to Overdue…
    await user.click(screen.getByRole("tab", { name: /overdue/i }));
    await waitFor(() => expect(capturedStatuses).toContain("overdue"));

    // …then back to All — the latest request must carry no status filter.
    await user.click(screen.getByRole("tab", { name: /^all$/i }));
    await waitFor(() => expect(capturedStatuses[capturedStatuses.length - 1]).toBeNull());
    await waitFor(() => expect(screen.getByText("hb-ok")).toBeInTheDocument());
  });

  it("empty_state_shows_when_no_rows", async () => {
    mswServer.use(
      http.get("/api/v1/heartbeat", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("No heartbeats yet")).toBeInTheDocument());
  });
});
