import { render, screen } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { encodeConditionQ } from "@/lib/condition/serialize";
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
import { ActivityFeed } from "./ActivityFeed";

// Current year epoch for a date that trimDate will format as "Today HH:mm"
// (not a relative "Xs/Xm" format — that's formatRelativeTime).
const NOW_EPOCH = Math.floor(Date.now() / 1000);

function setup() {
  const root = createRootRoute({ component: () => <Outlet /> });
  // We need a route for /web/alerts so <Link to="/web/alerts"> resolves.
  const alertsRoute = createRoute({
    getParentRoute: () => root,
    path: "/web/alerts",
    component: () => <div>alerts</div>,
  });
  const feedRoute = createRoute({
    getParentRoute: () => root,
    path: "/web/dashboard",
    component: ActivityFeed,
  });
  const tree = root.addChildren([alertsRoute, feedRoute]);
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

describe("ActivityFeed", () => {
  it("lists recent user actions with trimDate and a link to the alert", async () => {
    mswServer.use(
      http.get("/api/v1/comment", () =>
        HttpResponse.json({
          data: [
            {
              uid: "c1",
              record_uid: "alert-abc",
              type: "ack",
              user: "alice",
              message: "ok",
              date_epoch: NOW_EPOCH,
            },
          ],
          meta: { count: 1, limit: 15, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    expect(await screen.findByText("acknowledged")).toBeInTheDocument();
    expect(screen.getByText("alice")).toBeInTheDocument();
    // trimDate with a current-year date should NOT produce a relative "Xs/Xm/Xh/Xd" string
    expect(screen.queryByText(/^\d+[smhd]$/)).not.toBeInTheDocument();
    const links = screen.getAllByRole("link");
    expect(links[0]!.getAttribute("href")).toContain("/web/alerts");
  });

  it("requests only attributed (real-user) comments via an EXISTS user filter", async () => {
    let capturedUrl = "";
    mswServer.use(
      http.get("/api/v1/comment", ({ request }) => {
        capturedUrl = request.url;
        return HttpResponse.json({ data: [], meta: { count: 0, limit: 15, offset: 0, total: 0 } });
      }),
    );
    setup();
    await screen.findByText("No recent activity.");
    const q = new URL(capturedUrl).searchParams.get("q");
    expect(q).toBe(encodeConditionQ({ type: "EXISTS", field: "user" }));
  });

  it("shows an empty state when there are no actions", async () => {
    mswServer.use(
      http.get("/api/v1/comment", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 15, offset: 0, total: 0 },
        }),
      ),
    );
    setup();
    expect(await screen.findByText(/no recent activity/i)).toBeInTheDocument();
  });
});
