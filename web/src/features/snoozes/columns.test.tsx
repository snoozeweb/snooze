import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { snoozeColumns } from "./columns";
import type { Snooze } from "./types";

const statusCol = snoozeColumns.find((c) => c.id === "window_status")!;
const remainingCol = snoozeColumns.find((c) => c.id === "remaining_seconds")!;

const base: Snooze = { name: "x", enabled: true };

describe("snooze columns — disabled rules", () => {
  it("badges a disabled rule 'disabled', not its underlying window lifecycle", () => {
    render(<>{statusCol.cell({ ...base, enabled: false, window_status: "active" })}</>);
    expect(screen.getByText("disabled")).toBeInTheDocument();
    expect(screen.queryByText("active")).not.toBeInTheDocument();
  });

  it("shows the real window status when the rule is enabled", () => {
    render(<>{statusCol.cell({ ...base, enabled: true, window_status: "active" })}</>);
    expect(screen.getByText("active")).toBeInTheDocument();
    expect(screen.queryByText("disabled")).not.toBeInTheDocument();
  });

  it("shows no countdown for a disabled rule even with remaining_seconds set", () => {
    render(
      <>
        {remainingCol.cell({
          ...base,
          enabled: false,
          window_status: "active",
          remaining_seconds: 3600,
        })}
      </>,
    );
    expect(screen.getByText("—")).toBeInTheDocument();
  });
});
