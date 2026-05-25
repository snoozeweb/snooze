import { fireEvent, render, screen } from "@testing-library/react";
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
      <DataTable data={sample} columns={columns} rowKey={(r) => r.id} onRowOpen={onRowOpen} />,
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

  it("selectable: shift-click selects an inclusive range from the last anchor", () => {
    let current: Set<string> = new Set<string>();
    const onSelectionChange = (next: Set<string>) => {
      current = next;
    };
    const { rerender } = render(
      <DataTable
        data={sample}
        columns={columns}
        rowKey={(r) => r.id}
        selectable
        selectedKeys={current}
        onSelectionChange={onSelectionChange}
      />,
    );
    // Anchor: plain click on row 1 (id "1").
    const cells = screen
      .getAllByRole("checkbox", { name: /select row/i })
      .map((b) => b.closest("td")!);
    fireEvent.click(cells[0]!);
    rerender(
      <DataTable
        data={sample}
        columns={columns}
        rowKey={(r) => r.id}
        selectable
        selectedKeys={current}
        onSelectionChange={onSelectionChange}
      />,
    );
    // Shift+click on row 3 (id "3") should grow the selection to {1, 2, 3}.
    const cellsAfter = screen
      .getAllByRole("checkbox", { name: /select row/i })
      .map((b) => b.closest("td")!);
    fireEvent.click(cellsAfter[2]!, { shiftKey: true });
    expect(current.has("1")).toBe(true);
    expect(current.has("2")).toBe(true);
    expect(current.has("3")).toBe(true);
    expect(current.size).toBe(3);
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

  describe("context menu", () => {
    it("right-click on a row opens the context menu", () => {
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          contextMenuItems={() => [
            { key: "open", label: "Open", onSelect: vi.fn() },
            { key: "copy", label: "Copy as JSON", onSelect: vi.fn() },
          ]}
        />,
      );
      expect(screen.queryByRole("menu", { name: /row context menu/i })).toBeNull();
      fireEvent.contextMenu(screen.getByText("alpha"));
      expect(screen.getByRole("menu", { name: /row context menu/i })).toBeInTheDocument();
      expect(screen.getByRole("menuitem", { name: /open/i })).toBeInTheDocument();
      expect(screen.getByRole("menuitem", { name: /copy as json/i })).toBeInTheDocument();
    });

    it("renders icons next to labels when provided", () => {
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          contextMenuItems={() => [
            { key: "open", label: "Open", icon: "eye", onSelect: vi.fn() },
            { key: "delete", label: "Delete", icon: "trash", danger: true, onSelect: vi.fn() },
          ]}
        />,
      );
      fireEvent.contextMenu(screen.getByText("alpha"));
      const items = screen.getAllByRole("menuitem");
      expect(items.length).toBe(2);
      expect(items[0]!.querySelector("svg")).not.toBeNull();
      expect(items[1]!.querySelector("svg")).not.toBeNull();
    });

    it("clicking a menu item calls onSelect and closes the menu", async () => {
      const handler = vi.fn();
      const user = userEvent.setup();
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          contextMenuItems={(row) => [
            { key: "open", label: `Open ${row.name}`, onSelect: handler },
          ]}
        />,
      );
      fireEvent.contextMenu(screen.getByText("alpha"));
      await user.click(screen.getByRole("menuitem", { name: /open alpha/i }));
      expect(handler).toHaveBeenCalledTimes(1);
      expect(screen.queryByRole("menu", { name: /row context menu/i })).toBeNull();
    });

    it("passes the right row to the context-menu factory", () => {
      const factory = vi.fn(() => [{ key: "open", label: "Open", onSelect: vi.fn() }]);
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          contextMenuItems={factory}
        />,
      );
      fireEvent.contextMenu(screen.getByText("beta"));
      expect(factory).toHaveBeenCalledWith(sample[1]);
    });

    it("Escape closes the menu", () => {
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          contextMenuItems={() => [{ key: "open", label: "Open", onSelect: vi.fn() }]}
        />,
      );
      fireEvent.contextMenu(screen.getByText("alpha"));
      expect(screen.getByRole("menu", { name: /row context menu/i })).toBeInTheDocument();
      fireEvent.keyDown(document, { key: "Escape" });
      expect(screen.queryByRole("menu", { name: /row context menu/i })).toBeNull();
    });

    it("outside click closes the menu", () => {
      render(
        <div>
          <button type="button">outside</button>
          <DataTable
            data={sample}
            columns={columns}
            rowKey={(r) => r.id}
            contextMenuItems={() => [{ key: "open", label: "Open", onSelect: vi.fn() }]}
          />
        </div>,
      );
      fireEvent.contextMenu(screen.getByText("alpha"));
      expect(screen.getByRole("menu", { name: /row context menu/i })).toBeInTheDocument();
      fireEvent.mouseDown(screen.getByRole("button", { name: /outside/i }));
      expect(screen.queryByRole("menu", { name: /row context menu/i })).toBeNull();
    });

    it("does not attach onContextMenu when contextMenuItems is omitted", () => {
      render(<DataTable data={sample} columns={columns} rowKey={(r) => r.id} />);
      fireEvent.contextMenu(screen.getByText("alpha"));
      expect(screen.queryByRole("menu", { name: /row context menu/i })).toBeNull();
    });

    it("Enter activates the highlighted item", () => {
      const first = vi.fn();
      const second = vi.fn();
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          contextMenuItems={() => [
            { key: "a", label: "First", onSelect: first },
            { key: "b", label: "Second", onSelect: second },
          ]}
        />,
      );
      fireEvent.contextMenu(screen.getByText("alpha"));
      fireEvent.keyDown(document, { key: "ArrowDown" });
      fireEvent.keyDown(document, { key: "Enter" });
      expect(second).toHaveBeenCalled();
      expect(first).not.toHaveBeenCalled();
    });
  });

  describe("row expansion", () => {
    it("does not render the expand-chevron column when renderExpanded is omitted", () => {
      render(<DataTable data={sample} columns={columns} rowKey={(r) => r.id} />);
      expect(screen.queryByRole("button", { name: /expand row/i })).toBeNull();
    });

    it("renders a chevron toggle per row when renderExpanded is provided", () => {
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderExpanded={(r) => <div>details for {r.name}</div>}
        />,
      );
      const toggles = screen.getAllByRole("button", { name: /expand row/i });
      expect(toggles.length).toBe(sample.length);
    });

    it("clicking the chevron toggles expansion without triggering onRowOpen", async () => {
      const onRowOpen = vi.fn();
      const user = userEvent.setup();
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          onRowOpen={onRowOpen}
          renderExpanded={(r) => <div data-testid={`exp-${r.id}`}>details for {r.name}</div>}
        />,
      );
      expect(screen.queryByTestId("exp-2")).toBeNull();
      const toggles = screen.getAllByRole("button", { name: /expand row/i });
      await user.click(toggles[1]!);
      expect(screen.getByTestId("exp-2")).toBeInTheDocument();
      expect(onRowOpen).not.toHaveBeenCalled();
      await user.click(toggles[1]!);
      expect(screen.queryByTestId("exp-2")).toBeNull();
    });

    it("allows multiple rows to be expanded simultaneously", async () => {
      const user = userEvent.setup();
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderExpanded={(r) => <div data-testid={`exp-${r.id}`}>{r.name}</div>}
        />,
      );
      const toggles = screen.getAllByRole("button", { name: /expand row/i });
      await user.click(toggles[0]!);
      await user.click(toggles[2]!);
      expect(screen.getByTestId("exp-1")).toBeInTheDocument();
      expect(screen.getByTestId("exp-3")).toBeInTheDocument();
      expect(screen.queryByTestId("exp-2")).toBeNull();
    });

    it("clicking the row body still calls onRowOpen", async () => {
      const onRowOpen = vi.fn();
      const user = userEvent.setup();
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          onRowOpen={onRowOpen}
          renderExpanded={(r) => <div>details for {r.name}</div>}
        />,
      );
      await user.click(screen.getByText("beta"));
      expect(onRowOpen).toHaveBeenCalledWith(sample[1]);
    });

    it("fires onExpandedChange with the current set of expanded keys", async () => {
      const onExpandedChange = vi.fn();
      const user = userEvent.setup();
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderExpanded={(r) => <div>details for {r.name}</div>}
          onExpandedChange={onExpandedChange}
        />,
      );
      // Initial mount fires once with the empty default — consumers treat
      // that as "nothing is expanded" so it's harmless.
      const sizes = () =>
        onExpandedChange.mock.calls.map(([keys]) => (keys as ReadonlySet<string>).size);
      expect(sizes()).toEqual([0]);

      const toggles = screen.getAllByRole("button", { name: /expand row/i });
      await user.click(toggles[0]!);
      expect(sizes()).toEqual([0, 1]);

      await user.click(toggles[2]!);
      expect(sizes()).toEqual([0, 1, 2]);

      await user.click(toggles[0]!);
      expect(sizes()).toEqual([0, 1, 2, 1]);

      const last = onExpandedChange.mock.calls.at(-1)?.[0] as ReadonlySet<string>;
      expect([...last]).toEqual(["3"]);
    });
  });
});
