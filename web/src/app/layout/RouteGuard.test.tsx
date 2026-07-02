import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { authStore } from "@/lib/auth/store";
import { RouteGuard } from "./RouteGuard";

// Drive the current path; useNavigate is only reached by the AccessDenied CTA.
const location = { pathname: "/web/admin/users" };
vi.mock("@tanstack/react-router", () => ({
  useLocation: () => location,
  useNavigate: () => vi.fn(),
}));

function login(permissions: string[]) {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({ sub: "x", exp: Math.floor(Date.now() / 1000) + 3600, permissions }),
  );
  authStore.getState().login(`${header}.${body}.sig`);
}

describe("RouteGuard", () => {
  beforeEach(() => {
    localStorage.clear();
    authStore.getState().logout();
    location.pathname = "/web/admin/users";
  });
  afterEach(() => {
    localStorage.clear();
    authStore.getState().logout();
  });

  it("renders the page when the user holds a required permission", () => {
    login(["ro_user"]);
    render(
      <RouteGuard>
        <div>Users page</div>
      </RouteGuard>,
    );
    expect(screen.getByText("Users page")).toBeInTheDocument();
    expect(screen.queryByText(/access denied/i)).toBeNull();
  });

  it("shows Access denied (naming the permission) when the user lacks it", () => {
    login(["ro_rule"]); // unrelated permission
    render(
      <RouteGuard>
        <div>Users page</div>
      </RouteGuard>,
    );
    expect(screen.queryByText("Users page")).toBeNull();
    expect(screen.getByText(/access denied/i)).toBeInTheDocument();
    expect(screen.getByText(/ro_user or rw_user/i)).toBeInTheDocument();
  });

  it("gates the platform-tier Tenants route with the same predicate as the nav (rw_all is not enough)", () => {
    // rw_all would pass a naive hasAnyPermission check, but the Tenants route is
    // platform-tier: the nav hides it for a non-default-tenant / wildcard-only
    // holder, so RouteGuard must deny it too (no more permissive than the nav).
    location.pathname = "/web/admin/tenants";
    login(["rw_all"]);
    render(
      <RouteGuard>
        <div>Tenants page</div>
      </RouteGuard>,
    );
    expect(screen.queryByText("Tenants page")).toBeNull();
    expect(screen.getByText(/access denied/i)).toBeInTheDocument();
  });

  it("renders routes with no permission requirement unchanged", () => {
    location.pathname = "/web/profile"; // not in NAV_ITEMS → no gating
    login([]);
    render(
      <RouteGuard>
        <div>Profile page</div>
      </RouteGuard>,
    );
    expect(screen.getByText("Profile page")).toBeInTheDocument();
  });
});
