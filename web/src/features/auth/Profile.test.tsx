import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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
    mswServer.use(http.get("/api/v1/user/me/apikeys", () => HttpResponse.json({ data: [] })));
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

  it("explains a password mismatch in text (not colour alone) and wires it to the field", async () => {
    loginWith(["rw_rule"], "local");
    const user = userEvent.setup();
    setup();
    await user.type(screen.getByLabelText(/^new password$/i), "newpass1");
    await user.type(screen.getByLabelText(/confirm new password/i), "different");
    const msg = await screen.findByText(/passwords do not match/i);
    expect(msg).toBeInTheDocument();
    // The confirm field points at the message via aria-describedby.
    const confirm = screen.getByLabelText(/confirm new password/i);
    expect(confirm.getAttribute("aria-describedby")).toBe(msg.id);
    // Submit stays blocked while they mismatch.
    expect(screen.getByRole("button", { name: /update password/i })).toBeDisabled();
  });

  it("change-password form is hidden for non-local accounts", () => {
    loginWith(["rw_rule"], "ldap");
    setup();
    expect(screen.queryByLabelText(/current password/i)).toBeNull();
  });
});

describe("Profile — profile picture", () => {
  const PREVIEW = "data:image/png;base64,UFJFVklFVw==";
  let restoreCanvas: () => void = () => undefined;

  beforeEach(() => {
    localStorage.clear();
    authStore.getState().logout();
    mswServer.use(http.get("/api/v1/user/me/apikeys", () => HttpResponse.json({ data: [] })));
    // jsdom decodes no images and draws nothing: stand in for the decoder and
    // the canvas so the crop pipeline runs end to end.
    vi.stubGlobal(
      "createImageBitmap",
      vi.fn(() => Promise.resolve({ width: 640, height: 480, close: () => undefined })),
    );
    const getContext = vi
      .spyOn(HTMLCanvasElement.prototype, "getContext")
      .mockReturnValue({ drawImage: vi.fn() } as unknown as CanvasRenderingContext2D);
    const toDataURL = vi.spyOn(HTMLCanvasElement.prototype, "toDataURL").mockReturnValue(PREVIEW);
    restoreCanvas = () => {
      getContext.mockRestore();
      toDataURL.mockRestore();
    };
  });
  afterEach(() => {
    restoreCanvas();
    vi.unstubAllGlobals();
    localStorage.clear();
    authStore.getState().logout();
  });

  const file = (type = "image/jpeg", bytes = 2048) =>
    new File([new Uint8Array(bytes)], "me.jpg", { type });

  it("crops to a preview, saves it with PUT, and refreshes the directory", async () => {
    loginWith(["rw_rule"]);
    const puts: unknown[] = [];
    let peopleReads = 0;
    mswServer.use(
      http.get("/api/v1/people", () => {
        peopleReads++;
        return HttpResponse.json({ data: [{ name: "alice", method: "local" }] });
      }),
      http.put("/api/v1/user/me/avatar", async ({ request }) => {
        puts.push(await request.json());
        return HttpResponse.json({ version: "v2" });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(peopleReads).toBe(1));
    await user.upload(screen.getByLabelText("Choose a profile picture"), file());
    expect(
      await screen.findByRole("img", { name: "Preview of your new profile picture" }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Save picture" }));
    await waitFor(() => expect(puts).toEqual([{ data: PREVIEW }]));
    // Every avatar on the page reads the directory, so saving refetches it.
    await waitFor(() => expect(peopleReads).toBe(2));
    expect(screen.queryByRole("img", { name: /preview/i })).toBeNull();
  });

  it("refuses an unsupported type or an oversized file before uploading anything", async () => {
    loginWith(["rw_rule"]);
    const user = userEvent.setup({ applyAccept: false });
    setup();
    await user.upload(screen.getByLabelText("Choose a profile picture"), file("image/svg+xml"));
    expect(await screen.findByRole("alert")).toHaveTextContent(/PNG, JPEG or WebP/);
    await user.upload(
      screen.getByLabelText("Choose a profile picture"),
      file("image/png", 5 * 1024 * 1024 + 1),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(/over 5 MB/);
    expect(screen.queryByRole("button", { name: "Save picture" })).toBeNull();
  });

  it.each([
    [413, /too large for the server/],
    [422, /couldn't use that picture/],
  ])("explains a %i from the server and keeps the preview", async (status, copy) => {
    loginWith(["rw_rule"]);
    mswServer.use(
      http.put("/api/v1/user/me/avatar", () =>
        HttpResponse.json({ error: { code: "x", message: "no" } }, { status }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await user.upload(screen.getByLabelText("Choose a profile picture"), file());
    await user.click(await screen.findByRole("button", { name: "Save picture" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(copy);
    expect(screen.getByRole("img", { name: "Preview of your new profile picture" })).toBeVisible();
  });

  it("offers Remove only with a stored picture, and DELETEs it", async () => {
    loginWith(["rw_rule"]);
    let deletes = 0;
    mswServer.use(
      http.get("/api/v1/people", () =>
        HttpResponse.json({
          data: [{ name: "alice", method: "local", avatar_version: deletes ? "" : "v1" }],
        }),
      ),
      http.get("/api/v1/avatar/:method/:name", () =>
        HttpResponse.json({ name: "alice", method: "local", version: "v1", data: PREVIEW }),
      ),
      http.delete("/api/v1/user/me/avatar", () => {
        deletes++;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    setup();
    await user.click(await screen.findByRole("button", { name: "Remove" }));
    await waitFor(() => expect(deletes).toBe(1));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Remove" })).toBeNull());
    expect(screen.getByRole("button", { name: "Upload picture" })).toBeInTheDocument();
  });
});
