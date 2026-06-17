import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { mswServer } from "@/tests/msw/server";
import { authStore } from "@/lib/auth/store";
import { BottomNav } from "./BottomNav";

function loginWithPerms(perms: string[]) {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({ sub: "x", exp: Math.floor(Date.now() / 1000) + 3600, permissions: perms }),
  );
  authStore.getState().login(`${header}.${body}.sig`);
}

const PRIMARY_PERMS = ["ro_record", "ro_stats", "ro_snooze", "ro_rule"];

function setup(pathname = "/web/alerts") {
  const root = createRootRoute({ component: () => <BottomNav /> });
  // Register every route the bar can link to so <Link> resolves cleanly.
  const paths = [
    "/web/alerts",
    "/web/dashboard",
    "/web/snoozes",
    "/web/rules",
    "/web/notifications",
    "/web/profile",
    "/web/login",
  ];
  const children = paths.map((path) =>
    createRoute({ getParentRoute: () => root, path, component: () => <p>{path}</p> }),
  );
  const tree = root.addChildren(children);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-assignment */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: [pathname] }),
  }) as any;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-assignment */
}

describe("BottomNav", () => {
  afterEach(() => {
    localStorage.clear();
    authStore.getState().logout();
  });

  it("renders the four primary tabs plus More for a fully-permitted user", () => {
    loginWithPerms(PRIMARY_PERMS);
    setup();
    expect(screen.getByRole("link", { name: /alerts/i })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /dashboard/i })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /snoozes/i })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /rules/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /more/i })).toBeInTheDocument();
  });

  it("marks the active tab with aria-current=page", () => {
    loginWithPerms(PRIMARY_PERMS);
    setup("/web/dashboard");
    const link = screen.getByRole("link", { name: /dashboard/i });
    expect(link).toHaveAttribute("aria-current", "page");
  });

  it("shows fewer tabs when the user lacks permissions", () => {
    loginWithPerms(["ro_record"]);
    setup();
    expect(screen.getByRole("link", { name: /alerts/i })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /dashboard/i })).toBeNull();
    expect(screen.queryByRole("link", { name: /snoozes/i })).toBeNull();
    expect(screen.queryByRole("link", { name: /rules/i })).toBeNull();
    // More is always available.
    expect(screen.getByRole("button", { name: /more/i })).toBeInTheDocument();
  });

  it("renders the live alert-count badge on the Alerts tab", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 1, offset: 0, total: 7 } }),
      ),
    );
    loginWithPerms(PRIMARY_PERMS);
    setup();
    await waitFor(() => {
      expect(screen.getByLabelText("7 active alerts")).toBeInTheDocument();
    });
    expect(screen.getByLabelText("7 active alerts")).toHaveTextContent("7");
  });

  it("opens the More sheet when the More tab is tapped", async () => {
    loginWithPerms(PRIMARY_PERMS);
    const user = userEvent.setup();
    setup();
    const more = screen.getByRole("button", { name: /more/i });
    expect(more).toHaveAttribute("aria-expanded", "false");
    await user.click(more);
    expect(await screen.findByRole("dialog", { name: /menu/i })).toBeInTheDocument();
  });
});
