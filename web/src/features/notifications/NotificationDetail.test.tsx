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
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mswServer } from "@/tests/msw/server";
import { authStore } from "@/lib/auth/store";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { NotificationDetail } from "./NotificationDetail";
import type { Notification } from "./types";

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

/** Empty delivery log + empty audit log — the default backdrop. */
function stubEmpty() {
  mswServer.use(
    http.get("/api/v1/notificationlog", () =>
      HttpResponse.json({ data: [], meta: { count: 0, limit: 10, offset: 0, total: 0 } }),
    ),
    http.get("/api/v1/audit", () =>
      HttpResponse.json({ data: [], meta: { count: 0, limit: 5, offset: 0, total: 0 } }),
    ),
  );
}

function renderDetail(row: Notification, onEdit?: (uid: string) => void) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const root = createRootRoute({ component: () => <Outlet /> });
  const home = createRoute({
    getParentRoute: () => root,
    path: "/",
    component: () => <NotificationDetail row={row} {...(onEdit ? { onEdit } : {})} />,
  });
  const alerts = createRoute({
    getParentRoute: () => root,
    path: "/web/alerts",
    component: () => <div>alerts</div>,
    validateSearch: (raw: Record<string, unknown>) => raw,
  });
  const tree = root.addChildren([home, alerts]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: ["/"] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

const BASE: Notification = {
  uid: "n1",
  name: "page-on-call",
  enabled: true,
  condition: { type: "ALWAYS_TRUE" },
  actions: ["mail-oncall"],
};

describe("NotificationDetail", () => {
  beforeEach(() => loginWithPerms(["ro_notification", "ro_notificationlog"]));
  afterEach(() => authStore.getState().logout());

  it("opens on Deliveries, with Record and Audit log available behind it", async () => {
    stubEmpty();
    const user = userEvent.setup();
    renderDetail(BASE);

    // Deliveries leads: "did anyone get paged?" is why the drawer is open.
    expect(screen.getByRole("tab", { name: "Deliveries" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    await waitFor(() =>
      expect(screen.getByText(/Rows appear here the first time an alert matches/)).toBeVisible(),
    );

    await user.click(screen.getByRole("tab", { name: "Record" }));
    expect(await screen.findByText("Record", { selector: "h4" })).toBeVisible();

    await user.click(screen.getByRole("tab", { name: "Audit log" }));
    expect(await screen.findByText("Audit log", { selector: "h4" })).toBeVisible();
  });

  it("says nothing is ever sent when the notification has no actions", async () => {
    stubEmpty();
    const onEdit = vi.fn();
    const user = userEvent.setup();
    renderDetail({ ...BASE, actions: [] }, onEdit);

    expect(
      await screen.findByText("This notification has no actions, so nothing is ever sent."),
    ).toBeVisible();
    await user.click(screen.getByRole("button", { name: /add an action/i }));
    expect(onEdit).toHaveBeenCalledWith("n1");
  });

  it("explains that a disabled notification resumes when re-enabled", async () => {
    stubEmpty();
    renderDetail({ ...BASE, enabled: false });
    expect(
      await screen.findByText("Disabled — deliveries resume when it is enabled."),
    ).toBeVisible();
  });

  it("reads 'Never sent' until the first successful delivery", () => {
    stubEmpty();
    renderDetail(BASE);
    expect(screen.getByText("Never sent")).toBeVisible();
  });

  it("reads 'Sent 3×' with the last send time once the counters are stamped", () => {
    stubEmpty();
    const { container } = renderDetail({ ...BASE, hits: 3, last_sent: 1757340000 });
    expect(screen.getByText("Sent 3×")).toBeVisible();
    // last_sent renders through TimeCell, i.e. a semantic <time dateTime>.
    expect(container.querySelector("time")).toHaveAttribute("datetime");
  });

  it("hides the Deliveries tab and opens on Record without ro_notificationlog", async () => {
    // A role scoped to `ro_notification` can read the object but not its
    // history. The tab must not merely 403 in place — it must not exist, and
    // the drawer has to open on something the operator can actually read.
    authStore.getState().logout();
    loginWithPerms(["ro_notification"]);
    let logRequests = 0;
    mswServer.use(
      http.get("/api/v1/notificationlog", () => {
        logRequests += 1;
        return HttpResponse.json({ data: [], meta: { count: 0, limit: 10, offset: 0, total: 0 } });
      }),
      http.get("/api/v1/audit", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 5, offset: 0, total: 0 } }),
      ),
    );
    renderDetail(BASE);

    expect(screen.queryByRole("tab", { name: "Deliveries" })).toBeNull();
    expect(screen.getByRole("tab", { name: "Record" })).toHaveAttribute("aria-selected", "true");
    await waitFor(() => expect(screen.getByText("Record", { selector: "h4" })).toBeVisible());
    expect(logRequests).toBe(0);
  });

  it("shows the Deliveries tab for a role holding rw_notificationlog", () => {
    authStore.getState().logout();
    loginWithPerms(["ro_notification", "rw_notificationlog"]);
    stubEmpty();
    renderDetail(BASE);
    expect(screen.getByRole("tab", { name: "Deliveries" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });
});
