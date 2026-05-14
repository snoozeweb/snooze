import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { authStore } from "@/lib/auth/store";
import { Profile } from "./Profile";

function loginWith(perms: string[]) {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({
      sub: "alice",
      exp: Math.floor(Date.now() / 1000) + 3600,
      permissions: perms,
    }),
  );
  authStore.getState().login(`${header}.${body}.sig`);
}

function setup() {
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
  return render(<RouterProvider router={router as any} />);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument, @typescript-eslint/no-unsafe-assignment */
}

describe("Profile", () => {
  beforeEach(() => {
    localStorage.clear();
    authStore.getState().logout();
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

  it("Logout clears the store and routes to /web/login", async () => {
    loginWith(["rw_rule"]);
    const user = userEvent.setup();
    setup();
    expect(authStore.getState().isAuthenticated).toBe(true);
    await user.click(screen.getByRole("button", { name: /log out/i }));
    expect(authStore.getState().isAuthenticated).toBe(false);
    expect(await screen.findByText("Login page")).toBeInTheDocument();
  });
});
