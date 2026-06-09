import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import {
  createRootRoute,
  createRouter,
  RouterProvider,
  createMemoryHistory,
} from "@tanstack/react-router";
import { authStore } from "@/lib/auth/store";
import { Sidebar } from "./Sidebar";

function loginWithPerms(perms: string[]) {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({ sub: "x", exp: Math.floor(Date.now() / 1000) + 3600, permissions: perms }),
  );
  authStore.getState().login(`${header}.${body}.sig`);
}

function loginWithClaims(extra: Record<string, unknown>) {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({ sub: "x", exp: Math.floor(Date.now() / 1000) + 3600, ...extra }),
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
  "ro_tenant",
];

function setup(pathname = "/web/alerts") {
  const root = createRootRoute({ component: () => <Sidebar /> });
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-assignment */
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({ initialEntries: [pathname] }),
  }) as any;
  return render(<RouterProvider router={router} />);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-assignment */
}

describe("Sidebar", () => {
  afterEach(() => {
    localStorage.clear();
    authStore.getState().logout();
  });

  it("renders the three group labels", () => {
    loginWithPerms(ALL_PERMS);
    setup();
    expect(screen.getByText("Operate")).toBeInTheDocument();
    expect(screen.getByText("Configure")).toBeInTheDocument();
    expect(screen.getByText("Admin")).toBeInTheDocument();
  });

  it("renders all 13 nav items", () => {
    loginWithPerms(ALL_PERMS);
    setup();
    const expected = [
      "Alerts",
      "Dashboard",
      "Snoozes",
      "Rules",
      "Notifications",
      "Users",
      "Roles",
      "Environments",
      "Widgets",
      "Key-values",
      "Settings",
      "Status",
      "Tenants",
    ];
    for (const label of expected) {
      expect(screen.getByText(label)).toBeInTheDocument();
    }
  });

  it("marks the active item with aria-current=page", () => {
    loginWithPerms(["ro_stats"]);
    setup("/web/dashboard");
    const link = screen.getByRole("link", { name: /dashboard/i });
    expect(link).toHaveAttribute("aria-current", "page");
  });

  it("sidebar footer shows the tenant slug when present in claims", () => {
    const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
    const body = btoa(
      JSON.stringify({
        sub: "alice",
        tenant_id: "acme",
        exp: Math.floor(Date.now() / 1000) + 3600,
        permissions: [],
      }),
    );
    authStore.getState().login(`${header}.${body}.sig`);
    setup();
    expect(screen.getByText("acme")).toBeInTheDocument();
  });

  it("shows the Tenants nav item when the user has ro_tenant", () => {
    loginWithPerms(["ro_tenant"]);
    setup();
    expect(screen.getByText("Tenants")).toBeInTheDocument();
  });

  it("hides the Tenants nav item when the user lacks ro_tenant and rw_tenant", () => {
    loginWithPerms(["ro_record"]);
    setup();
    expect(screen.queryByText("Tenants")).toBeNull();
  });

  // The Tenants registry is a platform-tier route gated by RequirePlatformPerm:
  // literal ro_tenant/rw_tenant (rw_all does NOT count) AND default-tenant origin.
  // The sidebar must mirror that, or it shows a menu whose API 403s the user.
  it("hides the Tenants nav item for an rw_all admin without a literal tenant perm", () => {
    loginWithClaims({ tenant_id: "default", permissions: ["rw_all"] });
    setup();
    // rw_all still reveals ordinary admin items…
    expect(screen.getByText("Settings")).toBeInTheDocument();
    // …but not the platform-tier tenant registry.
    expect(screen.queryByText("Tenants")).toBeNull();
  });

  it("hides the Tenants nav item for a non-default tenant even with rw_tenant", () => {
    loginWithClaims({ tenant_id: "acme", permissions: ["rw_tenant"] });
    setup();
    expect(screen.queryByText("Tenants")).toBeNull();
  });

  it("shows the Tenants nav item for a default-tenant user with a literal ro_tenant", () => {
    loginWithClaims({ tenant_id: "default", permissions: ["ro_tenant"] });
    setup();
    expect(screen.getByText("Tenants")).toBeInTheDocument();
  });
});

describe("Sidebar permission filtering", () => {
  afterEach(() => {
    localStorage.clear();
    authStore.getState().logout();
  });

  it("hides items the user lacks permission for", () => {
    loginWithPerms(["ro_record"]);
    setup();
    expect(screen.getByText("Alerts")).toBeInTheDocument();
    expect(screen.queryByText("Rules")).toBeNull();
    expect(screen.queryByText("Settings")).toBeNull();
  });

  it("shows all items when the user has wide permissions", () => {
    loginWithPerms(ALL_PERMS);
    setup();
    expect(screen.getByText("Alerts")).toBeInTheDocument();
    expect(screen.getByText("Rules")).toBeInTheDocument();
    expect(screen.getByText("Settings")).toBeInTheDocument();
  });

  it("always shows items with no permission requirement", () => {
    loginWithPerms([]);
    setup();
    expect(screen.getByText("Status")).toBeInTheDocument();
  });
});
