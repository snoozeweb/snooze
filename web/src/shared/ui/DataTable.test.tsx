import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { DataTable, type ColumnDef } from "./DataTable";

type Row = { id: string; name: string; severity: string };
const sample: Row[] = [
  { id: "1", name: "alpha", severity: "critical" },
  { id: "2", name: "beta", severity: "warning" },
  { id: "3", name: "gamma", severity: "info" },
];
const columns: ColumnDef<Row>[] = [
  { id: "name", header: "Name", cell: (r) => r.name, sortable: true },
  { id: "severity", header: "Severity", cell: (r) => r.severity },
];

describe("DataTable", () => {
  it("renders columns + rows", () => {
    render(<DataTable data={sample} columns={columns} rowKey={(r) => r.id} />);
    expect(screen.getByText("Name")).toBeInTheDocument();
    expect(screen.getByText("alpha")).toBeInTheDocument();
    expect(screen.getByText("gamma")).toBeInTheDocument();
  });

  it("renders an empty state when no rows", () => {
    render(<DataTable data={[]} columns={columns} rowKey={(r) => r.id} />);
    expect(screen.getByText(/no items/i)).toBeInTheDocument();
  });

  it("renders skeleton rows when loading=true", () => {
    render(<DataTable data={[]} columns={columns} rowKey={(r) => r.id} loading />);
    expect(screen.queryByText(/no items/i)).toBeNull();
    expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
  });

  it("calls onRowOpen when a row is clicked", async () => {
    const onRowOpen = vi.fn();
    const user = userEvent.setup();
    render(
      <DataTable
        data={sample}
        columns={columns}
        rowKey={(r) => r.id}
        onRowOpen={onRowOpen}
      />,
    );
    await user.click(screen.getByText("beta"));
    expect(onRowOpen).toHaveBeenCalledWith(sample[1]);
  });

  it("selectable: header checkbox toggles all rows", async () => {
    const onSelectionChange = vi.fn();
    const user = userEvent.setup();
    render(
      <DataTable
        data={sample}
        columns={columns}
        rowKey={(r) => r.id}
        selectable
        selectedKeys={new Set()}
        onSelectionChange={onSelectionChange}
      />,
    );
    const allBox = screen.getByRole("checkbox", { name: /select all/i });
    await user.click(allBox);
    expect(onSelectionChange).toHaveBeenCalled();
    const next = onSelectionChange.mock.calls.at(-1)?.[0] as Set<string>;
    expect(next.size).toBe(3);
  });

  it("selectable: per-row checkbox toggles one row", async () => {
    const onSelectionChange = vi.fn();
    const user = userEvent.setup();
    render(
      <DataTable
        data={sample}
        columns={columns}
        rowKey={(r) => r.id}
        selectable
        selectedKeys={new Set()}
        onSelectionChange={onSelectionChange}
      />,
    );
    const boxes = screen.getAllByRole("checkbox", { name: /select row/i });
    await user.click(boxes[1]!);
    const next = onSelectionChange.mock.calls.at(-1)?.[0] as Set<string>;
    expect(next.has("2")).toBe(true);
    expect(next.size).toBe(1);
  });

  it("sortable header calls serverSort.onChange when given serverSort", async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(
      <DataTable
        data={sample}
        columns={columns}
        rowKey={(r) => r.id}
        serverSort={{ sortBy: "name", order: "asc", onChange }}
      />,
    );
    await user.click(screen.getByRole("button", { name: /name/i }));
    expect(onChange).toHaveBeenCalledWith({ sortBy: "name", order: "desc" });
  });

  it("renders row-actions menu and fires onSelect", async () => {
    const handler = vi.fn();
    const user = userEvent.setup();
    render(
      <DataTable
        data={[sample[0]!]}
        columns={columns}
        rowKey={(r) => r.id}
        rowActions={(r) => [{ key: "edit", label: `Edit ${r.name}`, onSelect: handler }]}
      />,
    );
    const moreButtons = screen.getAllByRole("button", { name: /row actions/i });
    await user.click(moreButtons[0]!);
    await user.click(screen.getByRole("menuitem", { name: /edit alpha/i }));
    expect(handler).toHaveBeenCalled();
  });

  it("renders bulkActions slot only when selection > 0", () => {
    const { rerender } = render(
      <DataTable
        data={sample}
        columns={columns}
        rowKey={(r) => r.id}
        selectable
        selectedKeys={new Set()}
        bulkActions={(rows) => <span>Selected {rows.length}</span>}
      />,
    );
    expect(screen.queryByText(/^Selected/)).toBeNull();
    rerender(
      <DataTable
        data={sample}
        columns={columns}
        rowKey={(r) => r.id}
        selectable
        selectedKeys={new Set(["1", "2"])}
        bulkActions={(rows) => <span>Selected {rows.length}</span>}
      />,
    );
    expect(screen.getByText("Selected 2")).toBeInTheDocument();
  });
});
