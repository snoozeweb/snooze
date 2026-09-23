import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ViewSwitch } from "./ViewSwitch";

describe("ViewSwitch", () => {
  it("marks the view that is on and switches to the other", async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<ViewSwitch value="overview" onChange={onChange} />);

    const group = screen.getByRole("group", { name: "Dashboard view" });
    expect(screen.getByRole("button", { name: "Overview" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByRole("button", { name: "Analyses" })).toHaveAttribute(
      "aria-pressed",
      "false",
    );

    await user.click(screen.getByRole("button", { name: "Analyses" }));
    expect(onChange).toHaveBeenCalledWith("analyses");
    expect(group).toBeInTheDocument();
  });

  // The segment says how much is behind it before it is opened: "Analyses 3"
  // is a queue, "Analyses" is a place that may be empty.
  it("counts the analysed alerts on the Analyses segment, and only when there are some", () => {
    const { rerender } = render(
      <ViewSwitch value="overview" onChange={vi.fn()} analysedCount={3} />,
    );
    expect(screen.getByRole("button", { name: "Analyses 3" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Overview" })).toHaveTextContent(/^Overview$/);

    rerender(<ViewSwitch value="overview" onChange={vi.fn()} analysedCount={0} />);
    expect(screen.getByRole("button", { name: "Analyses" })).toBeInTheDocument();
  });
});

// The active segment's fill IS the focus colour in dark: --accent-solid and
// --focus-ring are both #ffb000 (styles/theme.dark.css). A bespoke amber
// outline pulled in to 1px off that fill is an amber ring on amber — the one
// segment an operator tabs to is the one whose focus they cannot see. The
// global ring in styles/base.css (2px at 2px offset, on the surface behind the
// control) is visible; the active segment adds a near-black inset rim on the
// fill itself so the state reads in both themes.
describe("focus is visible on the segment that is on", () => {
  const css = readFileSync(
    resolve(process.cwd(), "src/features/dashboard/ViewSwitch.module.css"),
    "utf8",
  );

  it("does not re-declare the global focus ring on the segment", () => {
    expect(css).not.toMatch(/\.segment:focus-visible/);
  });

  it("gives the active segment a ring drawn in the on-accent ink", () => {
    expect(css).toMatch(/\.segment\[data-active="true"\]:focus-visible/);
    expect(css).toMatch(/inset 0 0 0 2px var\(--accent-solid-fg\)/);
  });
});
