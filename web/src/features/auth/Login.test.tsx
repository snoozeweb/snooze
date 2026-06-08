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

function setup(returnTo?: string, searchParams?: Record<string, string>) {
  const root = createRootRoute({ component: () => <Outlet /> });
  const login = createRoute({
    getParentRoute: () => root,
    path: "/web/login",
    component: Login,
  });
  const alerts = createRoute({
    getParentRoute: () => root,
    path: "/web/alerts",
    component: () => <p>Alerts</p>,
  });
  const tree = root.addChildren([login, alerts]);

  // Build the initial URL with any extra search params
  let initialPath = "/web/login";
  const params = new URLSearchParams();
  if (returnTo) params.set("return_to", encodeURIComponent(returnTo));
  if (searchParams) {
    for (const [k, v] of Object.entries(searchParams)) {
      params.set(k, v);
    }
  }
  const qs = params.toString();
  if (qs) initialPath = `${initialPath}?${qs}`;

  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument, @typescript-eslint/no-unsafe-assignment */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: [initialPath] }),
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

function setupWithSearch(searchParams: Record<string, string>) {
  return setup(undefined, searchParams);
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
    expect(await screen.findByRole("tab", { name: "Local", selected: true })).toBeInTheDocument();
  });

  it("only renders backends advertised by /api/v1/login", async () => {
    mswServer.use(
      http.get("/api/v1/login", () => HttpResponse.json({ data: { backends: ["anonymous"] } })),
    );
    setup();
    expect(await screen.findByRole("tab", { name: "Anonymous" })).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "Local" })).not.toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "LDAP" })).not.toBeInTheDocument();
  });

  it("shows a message when no backends are enabled", async () => {
    mswServer.use(http.get("/api/v1/login", () => HttpResponse.json({ data: { backends: [] } })));
    setup();
    expect(await screen.findByText(/no authentication backend is enabled/i)).toBeInTheDocument();
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

  it("does not render the Organization dropdown by default (0 tenants)", async () => {
    setup();
    await screen.findByRole("tab", { name: "Local", selected: true });
    expect(screen.queryByRole("combobox", { name: /organization/i })).not.toBeInTheDocument();
  });

  it("shows an Organization dropdown when more than one tenant is listed", async () => {
    mswServer.use(
      http.get("/api/v1/login", () =>
        HttpResponse.json({
          data: {
            backends: ["local"],
            tenants: [
              { id: "acme", display_name: "Acme" },
              { id: "globex", display_name: "Globex" },
            ],
          },
        }),
      ),
    );
    setup();
    expect(await screen.findByRole("combobox", { name: /organization/i })).toBeInTheDocument();
  });

  it("uses a single listed tenant implicitly (no picker)", async () => {
    mswServer.use(
      http.get("/api/v1/login", () =>
        HttpResponse.json({
          data: {
            backends: ["local"],
            tenants: [{ id: "acme", display_name: "Acme" }],
          },
        }),
      ),
    );
    setup();
    await screen.findByLabelText(/username/i);
    expect(screen.queryByRole("combobox", { name: /organization/i })).not.toBeInTheDocument();
  });

  it("resolves ?key= and locks the org to that tenant", async () => {
    mswServer.use(
      http.get("/api/v1/login", () =>
        HttpResponse.json({ data: { backends: ["local"], tenants: [] } }),
      ),
      http.get("/api/v1/login/tenant", ({ request }) => {
        const url = new URL(request.url);
        if (url.searchParams.get("key") === "KEY-acme") {
          return HttpResponse.json({ data: { id: "acme", display_name: "Acme Corp" } });
        }
        return new HttpResponse(null, { status: 404 });
      }),
    );
    setupWithSearch({ key: "KEY-acme" });
    expect(await screen.findByText(/Acme Corp/)).toBeInTheDocument();
    expect(screen.queryByRole("combobox", { name: /organization/i })).not.toBeInTheDocument();
  });

  it("sends org slug when submitting with two listed tenants", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.get("/api/v1/login", () =>
        HttpResponse.json({
          data: {
            backends: ["local"],
            tenants: [
              { id: "acme", display_name: "Acme" },
              { id: "globex", display_name: "Globex" },
            ],
          },
        }),
      ),
      http.post("/api/v1/login/local", async ({ request }) => {
        bodies.push(await request.json());
        const now = Math.floor(Date.now() / 1000);
        return HttpResponse.json({
          token:
            btoa(JSON.stringify({ alg: "HS256", typ: "JWT" })) +
            "." +
            btoa(JSON.stringify({ sub: "alice", exp: now + 3600 })) +
            ".sig",
          expires_at: new Date((now + 3600) * 1000).toISOString(),
          method: "local",
        });
      }),
    );
    const user = userEvent.setup();
    setup();
    // Wait for dropdown to appear and select Globex
    const select = await screen.findByRole("combobox", { name: /organization/i });
    await user.selectOptions(select, "globex");
    await user.type(await screen.findByLabelText(/username/i), "alice");
    await user.type(screen.getByLabelText(/password/i), "pw");
    await user.click(screen.getByRole("button", { name: /sign in$/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect((bodies[0] as { org?: string }).org).toBe("globex");
  });
});
