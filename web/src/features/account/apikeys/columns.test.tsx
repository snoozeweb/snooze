import { render, screen } from "@testing-library/react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { makeApiKeyColumns } from "./columns";
import type { ApiKey } from "./types";

// Type-check: ApiKey must carry last_used_at and use_count (Step 1 — RED until types.ts updated).
// This assignment is a compile-time assertion; it runs at import time.
const _checkType: ApiKey = {
  uid: "x",
  owner: "u",
  name: "k",
  last_used_at: 1_000_000,
  use_count: 5,
};
void _checkType; // avoid unused-var lint

// Wrap renders in TooltipProvider because TimeCell renders a Tooltip.
function renderCell(colId: string, row: ApiKey) {
  const col = makeApiKeyColumns().find((c) => c.id === colId)!;
  return render(<TooltipProvider>{col.cell(row)}</TooltipProvider>);
}

const BASE: ApiKey = { uid: "u1", owner: "alice", name: "ci-bot" };

describe("last_used_at column", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 5, 30, 12, 0)); // 2026-06-30
  });
  afterEach(() => vi.useRealTimers());

  it("renders 'never' when last_used_at is absent", () => {
    renderCell("last_used_at", BASE);
    expect(screen.getByText("never")).toBeInTheDocument();
  });

  it("renders a <time> element with the ISO dateTime when last_used_at is set", () => {
    const epoch = Math.floor(new Date(2026, 5, 15, 9, 0).getTime() / 1000);
    const { container } = renderCell("last_used_at", { ...BASE, last_used_at: epoch });
    expect(container.querySelector("time")).not.toBeNull();
    expect(container.querySelector("time")!.getAttribute("dateTime")).toBe(
      new Date(epoch * 1000).toISOString(),
    );
  });

  it("is marked sortable", () => {
    const col = makeApiKeyColumns().find((c) => c.id === "last_used_at")!;
    expect(col.sortable).toBe(true);
  });
});

describe("use_count column", () => {
  it("renders 0 when use_count is absent", () => {
    renderCell("use_count", BASE);
    expect(screen.getByText("0")).toBeInTheDocument();
  });

  it("renders the count as a number", () => {
    renderCell("use_count", { ...BASE, use_count: 42 });
    expect(screen.getByText("42")).toBeInTheDocument();
  });
});
