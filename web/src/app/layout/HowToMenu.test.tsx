import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { mswServer } from "@/tests/msw/server";
import { HowToMenu } from "./HowToMenu";

function setup(pathname = "/web/alerts") {
  const root = createRootRoute({ component: HowToMenu });
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({ initialEntries: [pathname] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
    </QueryClientProvider>,
  );
}

describe("HowToMenu", () => {
  it("renders the How to button on every page", () => {
    setup("/web/rules");
    expect(screen.getByRole("button", { name: /how to/i })).toBeInTheDocument();
  });

  it("does not show danger badges or fetch on non-alerts pages", async () => {
    let fetched = false;
    mswServer.use(
      http.get("/api/v1/action", () => {
        fetched = true;
        return HttpResponse.json({ data: [], meta: { count: 0, limit: 1, offset: 0, total: 0 } });
      }),
      http.get("/api/v1/notification", () => {
        fetched = true;
        return HttpResponse.json({ data: [], meta: { count: 0, limit: 1, offset: 0, total: 0 } });
      }),
    );
    setup("/web/rules");
    await new Promise((r) => setTimeout(r, 80));
    expect(fetched).toBe(false);
    expect(screen.queryByText(/no actions/i)).toBeNull();
    expect(screen.queryByText(/no notifications/i)).toBeNull();
  });

  it("shows No actions badge on alerts page when action count is zero", async () => {
    // The default MSW catch-all returns total:0, so action count is 0.
    // Override notification to non-zero so only the action badge appears.
    mswServer.use(
      http.get("/api/v1/notification", () =>
        HttpResponse.json({ data: [], meta: { count: 1, limit: 1, offset: 0, total: 1 } }),
      ),
    );
    setup("/web/alerts");
    await waitFor(() => expect(screen.getByText(/no actions/i)).toBeInTheDocument());
    expect(screen.queryByText(/no notifications/i)).toBeNull();
  });

  it("shows No notifications badge on alerts page when notif count is zero", async () => {
    mswServer.use(
      http.get("/api/v1/action", () =>
        HttpResponse.json({ data: [], meta: { count: 1, limit: 1, offset: 0, total: 1 } }),
      ),
    );
    setup("/web/alerts");
    await waitFor(() => expect(screen.getByText(/no notifications/i)).toBeInTheDocument());
    expect(screen.queryByText(/no actions/i)).toBeNull();
  });

  it("shows both badges when both counts are zero on alerts page", async () => {
    // Both default to total:0 via the catch-all handler.
    setup("/web/alerts");
    await waitFor(() => expect(screen.getByText(/no actions/i)).toBeInTheDocument());
    expect(screen.getByText(/no notifications/i)).toBeInTheDocument();
  });

  it("shows no badges when both counts are > 0 on alerts page", async () => {
    mswServer.use(
      http.get("/api/v1/action", () =>
        HttpResponse.json({ data: [], meta: { count: 2, limit: 1, offset: 0, total: 2 } }),
      ),
      http.get("/api/v1/notification", () =>
        HttpResponse.json({ data: [], meta: { count: 1, limit: 1, offset: 0, total: 1 } }),
      ),
    );
    setup("/web/alerts");
    await new Promise((r) => setTimeout(r, 100));
    expect(screen.queryByText(/no actions/i)).toBeNull();
    expect(screen.queryByText(/no notifications/i)).toBeNull();
  });

  it("clicking No actions badge opens SendAlertsDialog", async () => {
    mswServer.use(
      http.get("/api/v1/notification", () =>
        HttpResponse.json({ data: [], meta: { count: 1, limit: 1, offset: 0, total: 1 } }),
      ),
    );
    const user = userEvent.setup();
    setup("/web/alerts");
    await waitFor(() => screen.getByText(/no actions/i));
    await user.click(screen.getByText(/no actions/i));
    await waitFor(() =>
      expect(screen.getByRole("dialog", { name: /how to send alerts/i })).toBeInTheDocument(),
    );
  });

  it("dropdown Receive alerts item opens InjectAlertsDialog", async () => {
    const user = userEvent.setup();
    setup("/web/rules");
    await user.click(screen.getByRole("button", { name: /how to/i }));
    await user.click(screen.getByRole("menuitem", { name: /receive alerts/i }));
    expect(screen.getByRole("dialog", { name: /how to inject alerts/i })).toBeInTheDocument();
  });

  it("dropdown Send alerts item opens SendAlertsDialog", async () => {
    const user = userEvent.setup();
    setup("/web/rules");
    await user.click(screen.getByRole("button", { name: /how to/i }));
    await user.click(screen.getByRole("menuitem", { name: /send alerts/i }));
    await waitFor(() =>
      expect(screen.getByRole("dialog", { name: /how to send alerts/i })).toBeInTheDocument(),
    );
  });
});
