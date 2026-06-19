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

  it("does not open the row when a text selection is active (drag-select)", async () => {
    const onRowOpen = vi.fn();
    const user = userEvent.setup();
    // Simulate the user having dragged to highlight text: the trailing click
    // lands on the row, but a non-collapsed selection exists.
    const sel = { isCollapsed: false, toString: () => "highlighted" } as unknown as Selection;
    const spy = vi.spyOn(window, "getSelection").mockReturnValue(sel);
    render(
      <DataTable data={sample} columns={columns} rowKey={(r) => r.id} onRowOpen={onRowOpen} />,
    );
    await user.click(screen.getByText("beta"));
    expect(onRowOpen).not.toHaveBeenCalled();
    spy.mockRestore();
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

  it("overlays a count pill on the kebab and folds its label into the a11y name", () => {
    render(
      <DataTable
        data={[sample[0]!, sample[1]!]}
        columns={columns}
        rowKey={(r) => r.id}
        rowActions={() => [{ key: "edit", label: "Edit", onSelect: vi.fn() }]}
        // alpha gets a badge of 2; beta gets none.
        rowActionsBadge={(r) => (r.id === "1" ? { count: 2, label: "2 comments" } : undefined)}
      />,
    );
    // The pill text renders only for the badged row.
    expect(screen.getByText("2")).toBeInTheDocument();
    // …and the kebab's accessible name carries the badge label.
    expect(screen.getByRole("button", { name: /row actions, 2 comments/i })).toBeInTheDocument();
    // The un-badged row keeps the plain "Row actions" name.
    const plain = screen
      .getAllByRole("button", { name: /^row actions$/i })
      .filter((b) => b.getAttribute("aria-label") === "Row actions");
    expect(plain.length).toBe(1);
  });

  it("renders no pill when rowActionsBadge returns a zero/undefined count", () => {
    render(
      <DataTable
        data={[sample[0]!]}
        columns={columns}
        rowKey={(r) => r.id}
        rowActions={() => [{ key: "edit", label: "Edit", onSelect: vi.fn() }]}
        rowActionsBadge={() => ({ count: 0 })}
      />,
    );
    expect(screen.getByRole("button", { name: /^row actions$/i })).toBeInTheDocument();
    expect(screen.queryByText("0")).toBeNull();
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

    it("adds a Copy item at the top when text is selected at right-click time", () => {
      const sel = { isCollapsed: false, toString: () => "abc" } as unknown as Selection;
      const spy = vi.spyOn(window, "getSelection").mockReturnValue(sel);
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          contextMenuItems={() => [{ key: "delete", label: "Delete", onSelect: vi.fn() }]}
        />,
      );
      fireEvent.contextMenu(screen.getByText("alpha"));
      const items = screen.getAllByRole("menuitem");
      expect(items[0]).toHaveTextContent("Copy");
      expect(screen.getByText("Delete")).toBeInTheDocument();
      spy.mockRestore();
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

    it("controlled expansion renders exactly expandedKeys and routes toggles through onExpandedChange", async () => {
      const onExpandedChange = vi.fn();
      const user = userEvent.setup();
      const { rerender } = render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderExpanded={(r) => <div data-testid={`exp-${r.id}`}>{r.name}</div>}
          expandedKeys={new Set(["2"])}
          onExpandedChange={onExpandedChange}
        />,
      );
      // Only the controlled key is expanded — internal state is bypassed.
      expect(screen.getByTestId("exp-2")).toBeInTheDocument();
      expect(screen.queryByTestId("exp-1")).toBeNull();

      // Clicking another chevron must NOT self-expand; it asks the parent.
      const toggles = screen.getAllByRole("button", { name: /expand row/i });
      await user.click(toggles[0]!);
      expect(screen.queryByTestId("exp-1")).toBeNull();
      const next = onExpandedChange.mock.calls.at(-1)?.[0] as ReadonlySet<string>;
      expect([...next].sort()).toEqual(["1", "2"]);

      // Parent applies the new set → row 1 now renders expanded.
      rerender(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderExpanded={(r) => <div data-testid={`exp-${r.id}`}>{r.name}</div>}
          expandedKeys={new Set(["1", "2"])}
          onExpandedChange={onExpandedChange}
        />,
      );
      expect(screen.getByTestId("exp-1")).toBeInTheDocument();
      expect(screen.getByTestId("exp-2")).toBeInTheDocument();
    });
  });

  describe("quick actions", () => {
    it("renders a quick-action IconButton per row and fires onSelect", async () => {
      const handler = vi.fn();
      const user = userEvent.setup();
      render(
        <DataTable
          data={[sample[0]!]}
          columns={columns}
          rowKey={(r) => r.id}
          quickActions={(r) => [
            { key: "ack", label: `Ack ${r.name}`, icon: "check", onSelect: handler },
          ]}
        />,
      );
      const btn = screen.getByRole("button", { name: /ack alpha/i });
      await user.click(btn);
      expect(handler).toHaveBeenCalledTimes(1);
    });

    it("does not render quick actions when the prop is omitted", () => {
      render(<DataTable data={sample} columns={columns} rowKey={(r) => r.id} />);
      expect(screen.queryByRole("button", { name: /ack/i })).toBeNull();
    });

    it("renders quick actions alongside the kebab without replacing it", () => {
      render(
        <DataTable
          data={[sample[0]!]}
          columns={columns}
          rowKey={(r) => r.id}
          quickActions={(r) => [{ key: "ack", label: `Ack ${r.name}`, onSelect: vi.fn() }]}
          rowActions={(r) => [{ key: "edit", label: `Edit ${r.name}`, onSelect: vi.fn() }]}
        />,
      );
      expect(screen.getByRole("button", { name: /ack alpha/i })).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /row actions/i })).toBeInTheDocument();
    });
  });

  describe("row accent", () => {
    it("sets data-accent and the --row-accent custom property when rowAccent returns a colour", () => {
      render(
        <DataTable
          data={[sample[0]!, sample[1]!]}
          columns={columns}
          rowKey={(r) => r.id}
          rowAccent={(r) => (r.id === "1" ? "var(--severity-critical)" : undefined)}
        />,
      );
      const accented = screen.getByText("alpha").closest("tr")!;
      expect(accented).toHaveAttribute("data-accent", "true");
      expect(accented.style.getPropertyValue("--row-accent")).toBe("var(--severity-critical)");

      const plain = screen.getByText("beta").closest("tr")!;
      expect(plain).not.toHaveAttribute("data-accent");
    });
  });

  describe("stale prop", () => {
    it("sets data-stale and aria-busy on the table when stale=true", () => {
      render(<DataTable data={sample} columns={columns} rowKey={(r) => r.id} stale />);
      const table = screen.getByRole("grid");
      expect(table).toHaveAttribute("data-stale", "true");
      expect(table).toHaveAttribute("aria-busy", "true");
    });

    it("does not set data-stale or aria-busy when stale is omitted", () => {
      render(<DataTable data={sample} columns={columns} rowKey={(r) => r.id} />);
      const table = screen.getByRole("grid");
      expect(table).not.toHaveAttribute("data-stale");
      expect(table).not.toHaveAttribute("aria-busy");
    });

    it("clears data-stale and aria-busy when stale switches from true to false", () => {
      const { rerender } = render(
        <DataTable data={sample} columns={columns} rowKey={(r) => r.id} stale />,
      );
      const table = screen.getByRole("grid");
      expect(table).toHaveAttribute("data-stale", "true");

      rerender(<DataTable data={sample} columns={columns} rowKey={(r) => r.id} stale={false} />);
      expect(table).not.toHaveAttribute("data-stale");
      expect(table).not.toHaveAttribute("aria-busy");
    });
  });

  describe("keyboard navigation", () => {
    it("j / k move the focused row down / up like ArrowDown / ArrowUp", () => {
      render(<DataTable data={sample} columns={columns} rowKey={(r) => r.id} />);
      const table = screen.getByRole("grid");
      table.focus();
      fireEvent.keyDown(table, { key: "j" });
      expect(screen.getByText("alpha").closest("tr")).toHaveAttribute("data-focused", "true");
      fireEvent.keyDown(table, { key: "j" });
      expect(screen.getByText("beta").closest("tr")).toHaveAttribute("data-focused", "true");
      fireEvent.keyDown(table, { key: "k" });
      expect(screen.getByText("alpha").closest("tr")).toHaveAttribute("data-focused", "true");
    });

    it("e toggles expansion of the focused row when renderExpanded is set", () => {
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderExpanded={(r) => <div data-testid={`exp-${r.id}`}>{r.name}</div>}
        />,
      );
      const table = screen.getByRole("grid");
      table.focus();
      fireEvent.keyDown(table, { key: "j" }); // focus row 1
      fireEvent.keyDown(table, { key: "e" });
      expect(screen.getByTestId("exp-1")).toBeInTheDocument();
      fireEvent.keyDown(table, { key: "e" });
      expect(screen.queryByTestId("exp-1")).toBeNull();
    });

    it("moving focus re-renders only the affected rows, not the whole table", () => {
      // Probe: count how many times each row's cell renders. With the per-row
      // memo, a j/k focus move should re-render only the row that gained focus
      // and the one that lost it — never every row.
      const renders: Record<string, number> = {};
      const probeColumns: ColumnDef<Row>[] = [
        {
          id: "name",
          header: "Name",
          cell: (r) => {
            renders[r.id] = (renders[r.id] ?? 0) + 1;
            return r.name;
          },
        },
      ];
      render(<DataTable data={sample} columns={probeColumns} rowKey={(r) => r.id} />);
      // Initial render: each row's cell ran once.
      expect(renders).toEqual({ "1": 1, "2": 1, "3": 1 });

      const table = screen.getByRole("grid");
      table.focus();
      fireEvent.keyDown(table, { key: "j" }); // focus row 1 (id "1")
      // Only row "1" changed (gained focus); rows "2"/"3" are untouched.
      expect(renders["1"]).toBe(2);
      expect(renders["2"]).toBe(1);
      expect(renders["3"]).toBe(1);

      fireEvent.keyDown(table, { key: "j" }); // focus row 2 (id "2")
      // Row "1" lost focus and row "2" gained it; row "3" still untouched.
      expect(renders["1"]).toBe(3);
      expect(renders["2"]).toBe(2);
      expect(renders["3"]).toBe(1);
    });

    it("rowKeyBindings fire for the focused row and skip reserved keys", () => {
      const ack = vi.fn();
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          rowKeyBindings={() => ({ a: ack })}
        />,
      );
      const table = screen.getByRole("grid");
      table.focus();
      fireEvent.keyDown(table, { key: "j" }); // focus row 1
      fireEvent.keyDown(table, { key: "a" });
      expect(ack).toHaveBeenCalledTimes(1);
    });

    it("rowKeyBindings do NOT fire when Ctrl is held (Ctrl+C, Ctrl+A, …)", () => {
      const comment = vi.fn();
      const ack = vi.fn();
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          rowKeyBindings={() => ({ c: comment, a: ack })}
        />,
      );
      const table = screen.getByRole("grid");
      table.focus();
      fireEvent.keyDown(table, { key: "j" }); // focus row 1
      // Ctrl+C and Ctrl+A must pass through without triggering the bindings.
      fireEvent.keyDown(table, { key: "c", ctrlKey: true });
      fireEvent.keyDown(table, { key: "a", ctrlKey: true });
      expect(comment).not.toHaveBeenCalled();
      expect(ack).not.toHaveBeenCalled();
    });

    it("rowKeyBindings do NOT fire when Meta (Cmd) is held", () => {
      const comment = vi.fn();
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          rowKeyBindings={() => ({ c: comment })}
        />,
      );
      const table = screen.getByRole("grid");
      table.focus();
      fireEvent.keyDown(table, { key: "j" });
      fireEvent.keyDown(table, { key: "c", metaKey: true });
      expect(comment).not.toHaveBeenCalled();
    });

    it("j/k vim aliases do NOT fire when Ctrl is held (Ctrl+J, Ctrl+K)", () => {
      render(<DataTable data={sample} columns={columns} rowKey={(r) => r.id} />);
      const table = screen.getByRole("grid");
      table.focus();
      // Ctrl+J and Ctrl+K must not move focus so global shortcuts (e.g. command
      // palette on Ctrl+K) are not intercepted by the table.
      fireEvent.keyDown(table, { key: "j", ctrlKey: true });
      fireEvent.keyDown(table, { key: "k", ctrlKey: true });
      // No row should be focused (focusedIndex stays at -1 initial value).
      const rows = screen
        .getAllByRole("row")
        .filter((r) => r.getAttribute("data-focused") === "true");
      expect(rows).toHaveLength(0);
    });

    it("e does NOT expand when Ctrl is held", () => {
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderExpanded={(r) => <div data-testid={`exp-${r.id}`}>{r.name}</div>}
        />,
      );
      const table = screen.getByRole("grid");
      table.focus();
      fireEvent.keyDown(table, { key: "j" }); // focus row 1
      fireEvent.keyDown(table, { key: "e", ctrlKey: true });
      expect(screen.queryByTestId("exp-1")).toBeNull();
    });
  });
});
