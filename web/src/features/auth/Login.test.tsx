import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { mswServer } from "@/tests/msw/server";
import { authStore } from "@/lib/auth/store";
import { Login } from "./Login";

function setup(returnTo?: string) {
  const root = createRootRoute({ component: () => <Outlet /> });
  const login = createRoute({ getParentRoute: () => root, path: "/web/login", component: Login });
  const alerts = createRoute({
    getParentRoute: () => root,
    path: "/web/alerts",
    component: () => <p>Alerts</p>,
  });
  const tree = root.addChildren([login, alerts]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument, @typescript-eslint/no-unsafe-assignment */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({
      initialEntries: [
        returnTo ? `/web/login?return_to=${encodeURIComponent(returnTo)}` : "/web/login",
      ],
    }),
  } as any);
  return render(<RouterProvider router={router as any} />);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument, @typescript-eslint/no-unsafe-assignment */
}

describe("Login", () => {
  beforeEach(() => {
    localStorage.clear();
    authStore.getState().logout();
  });
  afterEach(() => {
    localStorage.clear();
    authStore.getState().logout();
  });

  it("defaults to the Local tab", () => {
    setup();
    expect(screen.getByRole("tab", { name: "Local", selected: true })).toBeInTheDocument();
  });

  it("submits Local credentials and stores the returned token", async () => {
    const user = userEvent.setup();
    setup();
    await user.type(screen.getByLabelText(/username/i), "alice");
    await user.type(screen.getByLabelText(/password/i), "hunter2");
    await user.click(screen.getByRole("button", { name: /sign in$/i }));
    await waitFor(() => expect(authStore.getState().isAuthenticated).toBe(true));
  });

  it("Anonymous tab signs in without credentials", async () => {
    const user = userEvent.setup();
    setup();
    await user.click(screen.getByRole("tab", { name: "Anonymous" }));
    await user.click(screen.getByRole("button", { name: /continue as anonymous/i }));
    await waitFor(() => expect(authStore.getState().isAuthenticated).toBe(true));
  });

  it("shows the server error envelope when login fails", async () => {
    mswServer.use(
      http.post("/api/v1/login/local", () =>
        HttpResponse.json(
          { code: "invalid_credentials", detail: "Bad username or password" },
          { status: 401 },
        ),
      ),
    );
    const user = userEvent.setup();
    setup();
    await user.type(screen.getByLabelText(/username/i), "alice");
    await user.type(screen.getByLabelText(/password/i), "wrong");
    await user.click(screen.getByRole("button", { name: /sign in$/i }));
    expect(await screen.findByText(/bad username or password/i)).toBeInTheDocument();
    expect(authStore.getState().isAuthenticated).toBe(false);
  });
});
