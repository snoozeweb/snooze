import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { alertColumns } from "./columns";
import type { Record_ } from "./types";

/** Renders one column's cell for a row, the way DataTable would. */
function renderCell(id: string, row: Record_) {
  const column = alertColumns.find((c) => c.id === id);
  if (!column) throw new Error(`no column ${id}`);
  return render(<>{column.cell(row)}</>);
}

describe("severity cell", () => {
  it("marks an analysed alert with a dot beside the severity badge", () => {
    renderCell("severity", {
      uid: "r1",
      severity: "critical",
      agentic: { root_cause: { summary: "disk full", confidence: "high" } },
    } as Record_);
    expect(screen.getByText("Critical")).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Analysed · high confidence" })).toBeInTheDocument();
  });

  it("leaves an unanalysed alert's cell as it was — no placeholder", () => {
    renderCell("severity", { uid: "r1", severity: "critical" } as Record_);
    expect(screen.getByText("Critical")).toBeInTheDocument();
    expect(screen.queryByRole("img")).toBeNull();
  });

  it("keeps the trend arrow, with the dot after it", () => {
    const { container } = renderCell("severity", {
      uid: "r1",
      severity: "critical",
      trend_indication: "moreSevere",
      agentic: { root_cause: { summary: "disk full", confidence: "low" } },
    } as Record_);
    expect(screen.getByLabelText("Severity escalated")).toBeInTheDocument();
    const dot = screen.getByRole("img", { name: "Analysed · low confidence" });
    expect(dot).toHaveAttribute("data-tone", "critical");
    // Order inside the cell: badge, then trend, then dot.
    const cell = container.firstElementChild;
    expect(cell?.lastElementChild).toBe(dot);
  });
});
