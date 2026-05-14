import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  createRootRoute,
  createRouter,
  RouterProvider,
  createMemoryHistory,
} from "@tanstack/react-router";
import { Sidebar } from "./Sidebar";

function setup(pathname = "/web/alerts") {
  const root = createRootRoute({ component: () => <Sidebar /> });
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({ initialEntries: [pathname] }),
  });
  return render(<RouterProvider router={router} />);
}

describe("Sidebar", () => {
  it("renders the three group labels", () => {
    setup();
    expect(screen.getByText("Operate")).toBeInTheDocument();
    expect(screen.getByText("Configure")).toBeInTheDocument();
    expect(screen.getByText("Admin")).toBeInTheDocument();
  });

  it("renders all 12 nav items", () => {
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
    ];
    for (const label of expected) {
      expect(screen.getByText(label)).toBeInTheDocument();
    }
  });

  it("marks the active item with aria-current=page", () => {
    setup("/web/dashboard");
    const link = screen.getByRole("link", { name: /dashboard/i });
    expect(link).toHaveAttribute("aria-current", "page");
  });
});
