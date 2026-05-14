import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import {
  createRootRoute,
  createRouter,
  RouterProvider,
  createMemoryHistory,
} from "@tanstack/react-router";
import { CommandPalette } from "./CommandPalette";

function Harness() {
  const [open, setOpen] = useState(true);
  return <CommandPalette open={open} onOpenChange={setOpen} />;
}

function setup() {
  const root = createRootRoute({ component: Harness });
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({ initialEntries: ["/web/alerts"] }),
  });
  return render(<RouterProvider router={router} />);
}

describe("CommandPalette", () => {
  it("renders the search input on open", () => {
    setup();
    expect(screen.getByPlaceholderText(/jump to/i)).toBeInTheDocument();
  });

  it("filters items by query", async () => {
    const user = userEvent.setup();
    setup();
    await user.type(screen.getByPlaceholderText(/jump to/i), "alert");
    expect(screen.getByRole("option", { name: /alerts/i })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: /dashboard/i })).toBeNull();
  });

  it("shows 'No matches' when nothing matches", async () => {
    const user = userEvent.setup();
    setup();
    await user.type(screen.getByPlaceholderText(/jump to/i), "qwertyuiop");
    expect(screen.getByText(/no matches/i)).toBeInTheDocument();
  });
});
