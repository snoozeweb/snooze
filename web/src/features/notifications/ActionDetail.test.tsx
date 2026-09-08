import { render, screen, waitFor } from "@testing-library/react";
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
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { mswServer } from "@/tests/msw/server";
import { authStore } from "@/lib/auth/store";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import type { Condition } from "@/lib/condition/types";
import { ActionDetail } from "./ActionDetail";
import type { Action } from "./types";

function decodeQ(q: string | null): Condition | undefined {
  if (!q) return undefined;
  const b64 = q.replace(/-/g, "+").replace(/_/g, "/");
  return JSON.parse(atob(b64 + "=".repeat((4 - (b64.length % 4)) % 4))) as Condition;
}

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

/** Records every condition the delivery log was queried with. */
function stubLog() {
  const seen: (Condition | undefined)[] = [];
  mswServer.use(
    http.get("/api/v1/notificationlog", ({ request }) => {
      seen.push(decodeQ(new URL(request.url).searchParams.get("q")));
      return HttpResponse.json({ data: [], meta: { count: 0, limit: 10, offset: 0, total: 0 } });
    }),
    http.get("/api/v1/audit", () =>
      HttpResponse.json({ data: [], meta: { count: 0, limit: 5, offset: 0, total: 0 } }),
    ),
  );
  return seen;
}

function renderDetail(row: Action) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const root = createRootRoute({ component: () => <Outlet /> });
  const home = createRoute({
    getParentRoute: () => root,
    path: "/",
    component: () => <ActionDetail row={row} />,
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

const MAIL: Action = {
  uid: "a1",
  name: "mail-oncall",
  action: { selected: "mail", subcontent: { to: ["ops@example.com"], batch: true } },
};

describe("ActionDetail", () => {
  beforeEach(() => loginWithPerms(["ro_notification", "ro_notificationlog"]));
  afterEach(() => authStore.getState().logout());

  it("summarises the notifier, its config hint and whether it batches", () => {
    stubLog();
    renderDetail(MAIL);
    expect(screen.getByText("mail-oncall")).toBeVisible();
    expect(screen.getByText("mail")).toBeVisible();
    expect(screen.getByText(/to=ops@example\.com/)).toBeVisible();
    // Batch is a labelled yes/no, matching the Actions table's column.
    expect(screen.getByText("Batch")).toBeVisible();
    expect(screen.getByText("yes")).toBeVisible();
  });

  it("scopes the delivery log to this action by name", async () => {
    const seen = stubLog();
    renderDetail(MAIL);
    await waitFor(() => expect(seen.length).toBeGreaterThan(0));
    const cond = seen[0];
    expect(cond?.type).toBe("AND");
    const clauses =
      cond?.type === "AND"
        ? cond.args.flatMap((a) => ("field" in a && "value" in a ? [a] : []))
        : [];
    expect(clauses).toContainEqual({ type: "EQUALS", field: "action", value: "mail-oncall" });
  });

  it("explains that deliveries land here once a notification routes to it", async () => {
    stubLog();
    renderDetail(MAIL);
    expect(
      await screen.findByText(
        "Rows appear here once a notification routes an alert to this action.",
      ),
    ).toBeVisible();
  });

  it("hides the Deliveries tab and queries nothing without ro_notificationlog", () => {
    authStore.getState().logout();
    loginWithPerms(["ro_notification"]);
    const seen = stubLog();
    renderDetail({ uid: "a1", name: "mail-oncall", action: { selected: "mail" } });
    expect(screen.queryByRole("tab", { name: "Deliveries" })).toBeNull();
    expect(screen.getByRole("tab", { name: "Record" })).toHaveAttribute("aria-selected", "true");
    expect(seen).toHaveLength(0);
  });
});
