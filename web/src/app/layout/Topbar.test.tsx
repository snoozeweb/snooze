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
import { authStore } from "@/lib/auth/store";
import { Topbar } from "./Topbar";

function renderTopbar(props: Parameters<typeof Topbar>[0]) {
  const root = createRootRoute({ component: () => <Topbar {...props} /> });
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
  return render(<RouterProvider router={router} />);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-assignment */
}

function loginAs(sub: string) {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({ sub, exp: Math.floor(Date.now() / 1000) + 3600, permissions: [] }),
  );
  authStore.getState().login(`${header}.${body}.sig`);
}

describe("Topbar", () => {
  afterEach(() => {
    localStorage.clear();
    authStore.getState().logout();
  });

  it("renders the breadcrumb when given", () => {
    renderTopbar({ breadcrumb: "Alerts", onOpenPalette: () => undefined });
    expect(screen.getByText("Alerts")).toBeInTheDocument();
  });

  it("calls onOpenPalette when the search button is clicked", async () => {
    const handler = vi.fn();
    const user = userEvent.setup();
    renderTopbar({ onOpenPalette: handler });
    await user.click(screen.getByRole("button", { name: /open command palette/i }));
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("toggles the theme on the theme icon button", async () => {
    document.documentElement.setAttribute("data-theme", "dark");
    const user = userEvent.setup();
    renderTopbar({ onOpenPalette: () => undefined });
    await user.click(screen.getByRole("button", { name: /switch to light theme/i }));
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });

  it("user menu shows the logged-in username", async () => {
    loginAs("alice");
    const user = userEvent.setup();
    renderTopbar({ onOpenPalette: () => undefined });
    await user.click(screen.getByRole("button", { name: /signed in as alice/i }));
    expect(screen.getByRole("menuitem", { name: /profile.*alice/i })).toBeInTheDocument();
  });
});
