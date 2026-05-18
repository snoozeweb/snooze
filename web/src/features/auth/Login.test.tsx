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
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
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
  // Disable retries in tests — backend-list errors must not delay assertions.
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router as any} />
    </QueryClientProvider>,
  );
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

  it("defaults to the Local tab", async () => {
    setup();
    // The backend list is fetched async; the tab list only renders once it resolves.
    expect(
      await screen.findByRole("tab", { name: "Local", selected: true }),
    ).toBeInTheDocument();
  });

  it("only renders backends advertised by /api/v1/login", async () => {
    mswServer.use(
      http.get("/api/v1/login", () =>
        HttpResponse.json({ data: { backends: ["anonymous"] } }),
      ),
    );
    setup();
    expect(
      await screen.findByRole("tab", { name: "Anonymous" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "Local" })).not.toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "LDAP" })).not.toBeInTheDocument();
  });

  it("shows a message when no backends are enabled", async () => {
    mswServer.use(
      http.get("/api/v1/login", () =>
        HttpResponse.json({ data: { backends: [] } }),
      ),
    );
    setup();
    expect(
      await screen.findByText(/no authentication backend is enabled/i),
    ).toBeInTheDocument();
  });

  it("submits Local credentials and stores the returned token", async () => {
    const user = userEvent.setup();
    setup();
    await user.type(await screen.findByLabelText(/username/i), "alice");
    await user.type(screen.getByLabelText(/password/i), "hunter2");
    await user.click(screen.getByRole("button", { name: /sign in$/i }));
    await waitFor(() => expect(authStore.getState().isAuthenticated).toBe(true));
  });

  it("Anonymous tab signs in without credentials", async () => {
    const user = userEvent.setup();
    setup();
    await user.click(await screen.findByRole("tab", { name: "Anonymous" }));
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
    await user.type(await screen.findByLabelText(/username/i), "alice");
    await user.type(screen.getByLabelText(/password/i), "wrong");
    await user.click(screen.getByRole("button", { name: /sign in$/i }));
    expect(await screen.findByText(/bad username or password/i)).toBeInTheDocument();
    expect(authStore.getState().isAuthenticated).toBe(false);
  });
});
