import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { authStore } from "@/lib/auth/store";
import { MoreSheet } from "./MoreSheet";

function loginWithPerms(perms: string[]) {
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

const ALL_PERMS = [
  "ro_record",
  "ro_stats",
  "ro_snooze",
  "ro_rule",
  "ro_notification",
  "ro_user",
  "ro_role",
  "ro_environment",
  "ro_widget",
  "ro_kv",
  "ro_settings",
];

function setup(open = true) {
  const onOpenChange = vi.fn();
  const root = createRootRoute({
    component: () => <MoreSheet open={open} onOpenChange={onOpenChange} />,
  });
  const profile = createRoute({
    getParentRoute: () => root,
    path: "/web/profile",
    component: () => <p>Profile</p>,
  });
  const login = createRoute({
    getParentRoute: () => root,
    path: "/web/login",
    component: () => <p>Login</p>,
  });
  const tree = root.addChildren([profile, login]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-assignment */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: ["/"] }),
  }) as any;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const utils = render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-assignment */
  return { ...utils, onOpenChange };
}

describe("MoreSheet", () => {
  afterEach(() => {
    localStorage.clear();
    authStore.getState().logout();
  });

  it("lists the overflow nav items (not the four bottom-bar primaries)", () => {
    loginWithPerms(ALL_PERMS);
    setup();
    // Overflow includes Notifications and the admin items…
    expect(screen.getByRole("link", { name: /notifications/i })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /settings/i })).toBeInTheDocument();
    // …but never the primaries that live on the bar.
    expect(screen.queryByRole("link", { name: /^alerts$/i })).toBeNull();
    expect(screen.queryByRole("link", { name: /^dashboard$/i })).toBeNull();
  });

  it("shows Profile, a theme toggle, and Log out", () => {
    loginWithPerms(ALL_PERMS);
    setup();
    expect(screen.getByRole("button", { name: /profile.*alice/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /theme/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /log out/i })).toBeInTheDocument();
  });

  it("clears the auth session when Log out is chosen", async () => {
    loginWithPerms(ALL_PERMS);
    const user = userEvent.setup();
    setup();
    await user.click(screen.getByRole("button", { name: /log out/i }));
    expect(authStore.getState().isAuthenticated).toBe(false);
  });

  it("does not render its content when closed", () => {
    loginWithPerms(ALL_PERMS);
    setup(false);
    expect(screen.queryByRole("dialog", { name: /menu/i })).toBeNull();
  });
});
