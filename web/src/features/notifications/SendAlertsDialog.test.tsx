import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi } from "vitest";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { mswServer } from "@/tests/msw/server";
import { SendAlertsDialog } from "./SendAlertsDialog";

function setup(open = true) {
  const onOpenChange = vi.fn();
  const root = createRootRoute({
    component: () => <SendAlertsDialog open={open} onOpenChange={onOpenChange} />,
  });
  const notifications = createRoute({
    getParentRoute: () => root,
    path: "/web/notifications",
    component: () => <p>Notifications</p>,
  });
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: root.addChildren([notifications]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
    </QueryClientProvider>,
  );
  return { onOpenChange };
}

describe("SendAlertsDialog", () => {
  it("does not render when closed", () => {
    setup(false);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("does not fetch when closed", async () => {
    let fetched = false;
    mswServer.use(
      http.get("/api/v1/action", () => {
        fetched = true;
        return HttpResponse.json({ data: [], meta: { count: 0, limit: 1, offset: 0, total: 0 } });
      }),
    );
    setup(false);
    await new Promise((r) => setTimeout(r, 80));
    expect(fetched).toBe(false);
  });

  it("shows both steps in warn state when all counts are zero", async () => {
    mswServer.use(
      http.get("/api/v1/action", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 1, offset: 0, total: 0 } }),
      ),
      http.get("/api/v1/notification", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 1, offset: 0, total: 0 } }),
      ),
    );
    setup();
    await waitFor(() =>
      expect(screen.getAllByText("⚠ None configured")).toHaveLength(2),
    );
  });

  it("shows ok badge for step 1 when action count is > 0", async () => {
    mswServer.use(
      http.get("/api/v1/action", () =>
        HttpResponse.json({ data: [], meta: { count: 3, limit: 1, offset: 0, total: 3 } }),
      ),
      http.get("/api/v1/notification", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 1, offset: 0, total: 0 } }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("✓ 3 configured")).toBeInTheDocument());
    expect(screen.getByText("⚠ None configured")).toBeInTheDocument();
  });

  it("shows ok badge for step 2 when notification count is > 0", async () => {
    mswServer.use(
      http.get("/api/v1/action", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 1, offset: 0, total: 0 } }),
      ),
      http.get("/api/v1/notification", () =>
        HttpResponse.json({ data: [], meta: { count: 2, limit: 1, offset: 0, total: 2 } }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("✓ 2 configured")).toBeInTheDocument());
    expect(screen.getByText("⚠ None configured")).toBeInTheDocument();
  });

  it("Go to Actions button calls onOpenChange(false)", async () => {
    mswServer.use(
      http.get("/api/v1/action", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 1, offset: 0, total: 0 } }),
      ),
      http.get("/api/v1/notification", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 1, offset: 0, total: 0 } }),
      ),
    );
    const { onOpenChange } = setup();
    const user = userEvent.setup();
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /go to actions/i })).toBeInTheDocument(),
    );
    await user.click(screen.getByRole("button", { name: /go to actions/i }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("Close button calls onOpenChange(false)", async () => {
    mswServer.use(
      http.get("/api/v1/action", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 1, offset: 0, total: 0 } }),
      ),
      http.get("/api/v1/notification", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 1, offset: 0, total: 0 } }),
      ),
    );
    const { onOpenChange } = setup();
    const user = userEvent.setup();
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: /^close$/i }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});
