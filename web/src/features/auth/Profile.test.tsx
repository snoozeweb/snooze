import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
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
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { authStore } from "@/lib/auth/store";
import { Profile } from "./Profile";

function loginWith(perms: string[], method: string = "local") {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({
      sub: "alice",
      method,
      exp: Math.floor(Date.now() / 1000) + 3600,
      permissions: perms,
    }),
  );
  authStore.getState().login(`${header}.${body}.sig`);
}

function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const root = createRootRoute({ component: () => <Outlet /> });
  const profile = createRoute({
    getParentRoute: () => root,
    path: "/web/profile",
    component: Profile,
  });
  const login = createRoute({
    getParentRoute: () => root,
    path: "/web/login",
    component: () => <p>Login page</p>,
  });
  const tree = root.addChildren([profile, login]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument, @typescript-eslint/no-unsafe-assignment */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: ["/web/profile"] }),
  } as any);
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider delay={0}>
        <ToastProvider>
          <RouterProvider router={router as any} />
          <Toaster />
        </ToastProvider>
      </TooltipProvider>
    </QueryClientProvider>,
  );
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument, @typescript-eslint/no-unsafe-assignment */
}

describe("Profile", () => {
  beforeEach(() => {
    localStorage.clear();
    authStore.getState().logout();
    mswServer.use(
      http.get("/api/v1/user/me/apikeys", () =>
        HttpResponse.json({ data: [] }),
      ),
    );
  });
  afterEach(() => {
    localStorage.clear();
    authStore.getState().logout();
  });

  it("renders the username from claims", () => {
    loginWith(["rw_rule"]);
    setup();
    expect(screen.getByText("alice")).toBeInTheDocument();
  });

  it("renders one badge per permission", () => {
    loginWith(["rw_rule", "ro_record"]);
    setup();
    expect(screen.getByText("rw_rule")).toBeInTheDocument();
    expect(screen.getByText("ro_record")).toBeInTheDocument();
  });

  it("colours rw_ and ro_ permissions with the same (distinct) code as the roles table", () => {
    loginWith(["rw_rule", "ro_record"]);
    setup();
    // rw_* (warning) must not share a class with ro_* (info) — the Profile
    // page now reuses permissionBadgeVariant instead of a flat blue badge.
    const rw = screen.getByText("rw_rule").className;
    const ro = screen.getByText("ro_record").className;
    expect(rw).not.toBe(ro);
  });

  it("Logout clears the store and routes to /web/login", async () => {
    loginWith(["rw_rule"]);
    const user = userEvent.setup();
    setup();
    expect(authStore.getState().isAuthenticated).toBe(true);
    await user.click(screen.getByRole("button", { name: /log out/i }));
    expect(authStore.getState().isAuthenticated).toBe(false);
    expect(await screen.findByText("Login page")).toBeInTheDocument();
  });

  it("change-password form posts to /user/me/password for local accounts", async () => {
    loginWith(["rw_rule"], "local");
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/user/me/password", async ({ request }) => {
        bodies.push(await request.json());
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    setup();
    await user.type(screen.getByLabelText(/current password/i), "secret");
    await user.type(screen.getByLabelText(/^new password$/i), "newpass1");
    await user.type(screen.getByLabelText(/confirm new password/i), "newpass1");
    await user.click(screen.getByRole("button", { name: /update password/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toEqual({ current_password: "secret", password: "newpass1" });
  });

  it("change-password form is hidden for non-local accounts", () => {
    loginWith(["rw_rule"], "ldap");
    setup();
    expect(screen.queryByLabelText(/current password/i)).toBeNull();
  });
});
