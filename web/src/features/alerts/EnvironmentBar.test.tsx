import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import {
  createRootRoute,
  createRouter,
  RouterProvider,
  createMemoryHistory,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { mswServer } from "@/tests/msw/server";
import { authStore } from "@/lib/auth/store";
import { EnvironmentBar } from "./EnvironmentBar";

// ---------------------------------------------------------------------------
// Auth helpers (same pattern as Sidebar.test.tsx)
// ---------------------------------------------------------------------------

function loginWithPerms(perms: string[]) {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({ sub: "x", exp: Math.floor(Date.now() / 1000) + 3600, permissions: perms }),
  );
  authStore.getState().login(`${header}.${body}.sig`);
}

// ---------------------------------------------------------------------------
// MSW: stub /api/v1/environment to return one environment so the bar renders.
// Without at least one env, EnvironmentBar returns null immediately.
// ---------------------------------------------------------------------------

const stubEnvHandler = http.get("/api/v1/environment", () =>
  HttpResponse.json({
    data: [{ uid: "env-prod", name: "Production", color: "#3b82f6" }],
    meta: { count: 1, limit: 200, offset: 0, total: 1 },
  }),
);

// ---------------------------------------------------------------------------
// Render helper — needs QueryClientProvider (environments query) + a router
// context (EnvironmentBar renders a <Link> for the cog).
// ---------------------------------------------------------------------------

function setup() {
  const root = createRootRoute({
    component: () => <EnvironmentBar selected={[]} onChange={() => undefined} />,
  });
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-assignment */
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({ initialEntries: ["/web/alerts"] }),
  }) as any;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-assignment */
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("EnvironmentBar — Manage environments cog", () => {
  afterEach(() => {
    localStorage.clear();
    authStore.getState().logout();
  });

  it("shows the Manage environments link for a user with rw_environment", async () => {
    mswServer.use(stubEnvHandler);
    loginWithPerms(["rw_environment"]);
    setup();
    await waitFor(() => {
      expect(screen.getByRole("link", { name: /manage environments/i })).toBeInTheDocument();
    });
  });

  it("shows the Manage environments link for a user with rw_all (wildcard admin)", async () => {
    mswServer.use(stubEnvHandler);
    loginWithPerms(["rw_all"]);
    setup();
    await waitFor(() => {
      expect(screen.getByRole("link", { name: /manage environments/i })).toBeInTheDocument();
    });
  });

  it("hides the Manage environments link when the user lacks rw_environment and rw_all", async () => {
    mswServer.use(stubEnvHandler);
    loginWithPerms(["ro_environment", "ro_record"]);
    setup();
    // Wait for environments to load so the bar is rendered.
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "All" })).toBeInTheDocument();
    });
    expect(screen.queryByRole("link", { name: /manage environments/i })).toBeNull();
  });

  it("hides the Manage environments link when the user is not logged in", async () => {
    mswServer.use(stubEnvHandler);
    // No loginWithPerms call — claims are null.
    setup();
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "All" })).toBeInTheDocument();
    });
    expect(screen.queryByRole("link", { name: /manage environments/i })).toBeNull();
  });
});
