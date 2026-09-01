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

  it("renders errorState instead of emptyState when both are supplied", () => {
    render(
      <DataTable
        data={[]}
        columns={columns}
        rowKey={(r) => r.id}
        emptyState={<div>All clear</div>}
        errorState={<div>Can&apos;t reach the store</div>}
      />,
    );
    expect(screen.getByText(/can't reach the store/i)).toBeInTheDocument();
    expect(screen.queryByText(/all clear/i)).toBeNull();
  });

  it("keeps the rows (not errorState) when a failed refetch still has data", () => {
    render(
      <DataTable
        data={sample}
        columns={columns}
        rowKey={(r) => r.id}
        errorState={<div>Can&apos;t reach the store</div>}
      />,
    );
    expect(screen.getByText("alpha")).toBeInTheDocument();
    expect(screen.queryByText(/can't reach the store/i)).toBeNull();
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

  describe("hideBelow", () => {
    // jsdom can't evaluate container queries, so this only asserts the
    // `data-hide` attribute lands where DataTable.module.css's
    // `[data-hide="…"]` selectors expect it — on both the header cell and
    // every row's body cell for that column, and nowhere else.
    const tieredColumns: ColumnDef<Row>[] = [
      { id: "name", header: "Name", cell: (r) => r.name },
      { id: "severity", header: "Severity", cell: (r) => r.severity, hideBelow: "lg" },
    ];

    it("stamps data-hide on the th and every td for a column with hideBelow", () => {
      render(<DataTable data={sample} columns={tieredColumns} rowKey={(r) => r.id} />);
      const severityHeader = screen.getByRole("columnheader", { name: /severity/i });
      expect(severityHeader).toHaveAttribute("data-hide", "lg");

      const nameHeader = screen.getByRole("columnheader", { name: /^name$/i });
      expect(nameHeader).not.toHaveAttribute("data-hide");

      for (const row of sample) {
        const cell = screen.getByText(row.severity).closest("td")!;
        expect(cell).toHaveAttribute("data-hide", "lg");
        const nameCell = screen.getByText(row.name).closest("td")!;
        expect(nameCell).not.toHaveAttribute("data-hide");
      }
    });

    it("stamps data-hide on loading-skeleton cells for a column with hideBelow", () => {
      render(<DataTable data={[]} columns={tieredColumns} rowKey={(r) => r.id} loading />);
      const skeletonCells = screen.getAllByTestId("skeleton").map((s) => s.closest("td")!);
      expect(skeletonCells.some((td) => td.getAttribute("data-hide") === "lg")).toBe(true);
      expect(skeletonCells.some((td) => !td.hasAttribute("data-hide"))).toBe(true);
    });
  });

  describe("aria-sort", () => {
    const sortableColumns: ColumnDef<Row>[] = [
      { id: "name", header: "Name", cell: (r) => r.name, sortable: true },
      { id: "severity", header: "Severity", cell: (r) => r.severity, sortable: true },
    ];

    it("marks the active ascending column and reports 'none' on other sortable columns", () => {
      render(
        <DataTable
          data={sample}
          columns={sortableColumns}
          rowKey={(r) => r.id}
          serverSort={{ sortBy: "name", order: "asc", onChange: vi.fn() }}
        />,
      );
      expect(screen.getByRole("columnheader", { name: /name/i })).toHaveAttribute(
        "aria-sort",
        "ascending",
      );
      expect(screen.getByRole("columnheader", { name: /severity/i })).toHaveAttribute(
        "aria-sort",
        "none",
      );
    });

    it("marks the active column as descending when order is desc", () => {
      render(
        <DataTable
          data={sample}
          columns={sortableColumns}
          rowKey={(r) => r.id}
          serverSort={{ sortBy: "name", order: "desc", onChange: vi.fn() }}
        />,
      );
      expect(screen.getByRole("columnheader", { name: /name/i })).toHaveAttribute(
        "aria-sort",
        "descending",
      );
    });

    it("omits aria-sort entirely on non-sortable columns and when no serverSort is given", () => {
      render(<DataTable data={sample} columns={columns} rowKey={(r) => r.id} />);
      for (const h of screen.getAllByRole("columnheader")) {
        expect(h).not.toHaveAttribute("aria-sort");
      }
    });
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

  it("exposes a keyboard-shortcuts legend when keyboardHints are provided", async () => {
    const user = userEvent.setup();
    render(
      <DataTable
        data={sample}
        columns={columns}
        rowKey={(r) => r.id}
        selectable
        renderDetails={(r) => <div>{r.name} details</div>}
        onRowOpen={vi.fn()}
        keyboardHints={[
          { keys: "A", label: "Acknowledge" },
          { keys: "C", label: "Comment" },
        ]}
      />,
    );
    const trigger = screen.getByRole("button", { name: /keyboard shortcuts/i });
    await user.click(trigger);
    // Built-ins derived from the table's own capabilities…
    expect(await screen.findByText("Move between rows")).toBeInTheDocument();
    expect(screen.getByText("Select / deselect row")).toBeInTheDocument();
    expect(screen.getByText(/view details/i)).toBeInTheDocument();
    expect(screen.getByText("Clear selection, then focus")).toBeInTheDocument();
    // …plus the page-supplied row bindings.
    expect(screen.getByText("Acknowledge")).toBeInTheDocument();
    expect(screen.getByText("Comment")).toBeInTheDocument();
  });

  it("omits the keyboard-shortcuts legend when no keyboardHints are provided", () => {
    render(<DataTable data={sample} columns={columns} rowKey={(r) => r.id} selectable />);
    expect(screen.queryByRole("button", { name: /keyboard shortcuts/i })).toBeNull();
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

    it("does not attach onContextMenu when neither contextMenuItems nor renderDetails is set", () => {
      render(<DataTable data={sample} columns={columns} rowKey={(r) => r.id} />);
      fireEvent.contextMenu(screen.getByText("alpha"));
      expect(screen.queryByRole("menu", { name: /row context menu/i })).toBeNull();
    });

    it("prepends 'View details' ahead of the page's contextMenuItems when renderDetails is set, and opens the drawer", async () => {
      const user = userEvent.setup();
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={(r) => <div data-testid={`det-${r.id}`}>{r.name} details</div>}
          contextMenuItems={() => [{ key: "open", label: "Open", onSelect: vi.fn() }]}
        />,
      );
      fireEvent.contextMenu(screen.getByText("beta"));
      expect(screen.getByRole("menu", { name: /row context menu/i })).toBeInTheDocument();
      const items = screen.getAllByRole("menuitem");
      expect(items[0]).toHaveTextContent("View details");
      expect(items[1]).toHaveTextContent("Open");
      await user.click(screen.getByRole("menuitem", { name: /view details/i }));
      expect(screen.getByTestId("det-2")).toBeInTheDocument();
    });

    it("shows a right-click menu containing exactly 'View details' when renderDetails is set but contextMenuItems is omitted", () => {
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={(r) => <div>{r.name} details</div>}
        />,
      );
      fireEvent.contextMenu(screen.getByText("alpha"));
      const items = screen.getAllByRole("menuitem");
      expect(items).toHaveLength(1);
      expect(items[0]).toHaveTextContent("View details");
    });

    it("still leads with the selection Copy item ahead of the auto 'View details' entry", () => {
      const sel = { isCollapsed: false, toString: () => "abc" } as unknown as Selection;
      const spy = vi.spyOn(window, "getSelection").mockReturnValue(sel);
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={(r) => <div>{r.name} details</div>}
        />,
      );
      fireEvent.contextMenu(screen.getByText("alpha"));
      const items = screen.getAllByRole("menuitem");
      expect(items[0]).toHaveTextContent("Copy");
      expect(items[1]).toHaveTextContent("View details");
      spy.mockRestore();
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

  describe("detail drawer", () => {
    // The details API renders a modal drawer AFTER the table — there is no
    // leading toggle column anymore. Rows open it via the auto-appended
    // "View details" kebab item, the `E` shortcut, or a page-owned affordance.
    const renderDet = (r: Row) => <div data-testid={`det-${r.id}`}>details for {r.name}</div>;

    it("renders no leading inspect column and no drawer by default", () => {
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={renderDet}
        />,
      );
      expect(screen.queryByRole("button", { name: /inspect row/i })).toBeNull();
      expect(screen.queryByRole("dialog")).toBeNull();
    });

    it("renders no kebab column when neither rowActions nor renderDetails is set", () => {
      render(<DataTable data={sample} columns={columns} rowKey={(r) => r.id} />);
      expect(screen.queryByRole("button", { name: /row actions/i })).toBeNull();
    });

    it("auto-appends a 'View details' kebab item (even without rowActions) that opens the drawer", async () => {
      const onRowOpen = vi.fn();
      const user = userEvent.setup();
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          onRowOpen={onRowOpen}
          renderDetails={renderDet}
        />,
      );
      // The kebab column renders even though no rowActions were supplied.
      const kebabs = screen.getAllByRole("button", { name: /row actions/i });
      expect(kebabs.length).toBe(sample.length);
      await user.click(kebabs[1]!);
      await user.click(screen.getByRole("menuitem", { name: /view details/i }));
      expect(screen.getByTestId("det-2")).toBeInTheDocument();
      // Opening the drawer from the kebab must not also fire the row-open path.
      expect(onRowOpen).not.toHaveBeenCalled();
    });

    it("renders a per-row 'View details' quick action that opens the drawer", async () => {
      const user = userEvent.setup();
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={renderDet}
        />,
      );
      // The quick-actions column renders even though the page supplied no
      // quickActions — the built-in button is the row's visual cue that a
      // detail drawer exists.
      const buttons = screen.getAllByRole("button", { name: "View details" });
      expect(buttons.length).toBe(sample.length);
      await user.click(buttons[1]!);
      expect(screen.getByTestId("det-2")).toBeInTheDocument();
    });

    it("leads the quick-actions cluster with 'View details' before page quick actions", () => {
      render(
        <DataTable
          data={[sample[0]!]}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={renderDet}
          quickActions={(r) => [{ key: "ack", label: `Ack ${r.name}`, onSelect: vi.fn() }]}
        />,
      );
      const details = screen.getByRole("button", { name: "View details" });
      const ack = screen.getByRole("button", { name: "Ack alpha" });
      // Same cluster, details first.
      expect(details.compareDocumentPosition(ack) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    });

    it("prepends 'View details' before the page's own row actions", async () => {
      const user = userEvent.setup();
      render(
        <DataTable
          data={[sample[0]!]}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={renderDet}
          rowActions={(r) => [{ key: "edit", label: `Edit ${r.name}`, onSelect: vi.fn() }]}
        />,
      );
      await user.click(screen.getByRole("button", { name: /row actions/i }));
      const items = screen.getAllByRole("menuitem");
      expect(items[0]).toHaveTextContent("View details");
      expect(items[1]).toHaveTextContent("Edit alpha");
    });

    it("opens the drawer via the E shortcut on the focused row", () => {
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={renderDet}
        />,
      );
      const table = screen.getByRole("grid");
      table.focus();
      fireEvent.keyDown(table, { key: "ArrowDown" }); // focus row 1 (id "1")
      fireEvent.keyDown(table, { key: "e" });
      expect(screen.getByTestId("det-1")).toBeInTheDocument();
    });

    it("falls back to a 'Details' drawer title, and renders detailsTitle when supplied", async () => {
      const user = userEvent.setup();
      const { rerender } = render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={renderDet}
        />,
      );
      await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
      await user.click(screen.getByRole("menuitem", { name: /view details/i }));
      expect(screen.getByRole("dialog")).toHaveTextContent("Details");

      rerender(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={renderDet}
          detailsTitle={(r) => <span>Title: {r.name}</span>}
        />,
      );
      expect(screen.getByText("Title: alpha")).toBeInTheDocument();
    });

    it("prev/next retarget the drawer and disable at the ends", async () => {
      const user = userEvent.setup();
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={renderDet}
        />,
      );
      await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
      await user.click(screen.getByRole("menuitem", { name: /view details/i }));
      expect(screen.getByTestId("det-1")).toBeInTheDocument();
      // On the first row: Previous disabled, Next enabled; counter reads 1 / 3.
      expect(screen.getByText("1 / 3")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /previous row/i })).toBeDisabled();
      expect(screen.getByRole("button", { name: /next row/i })).toBeEnabled();

      await user.click(screen.getByRole("button", { name: /next row/i }));
      expect(screen.getByTestId("det-2")).toBeInTheDocument();
      expect(screen.queryByTestId("det-1")).toBeNull();

      await user.click(screen.getByRole("button", { name: /next row/i }));
      expect(screen.getByTestId("det-3")).toBeInTheDocument();
      // On the last row: Next disabled, Previous enabled.
      expect(screen.getByRole("button", { name: /next row/i })).toBeDisabled();
      expect(screen.getByRole("button", { name: /previous row/i })).toBeEnabled();

      await user.click(screen.getByRole("button", { name: /previous row/i }));
      expect(screen.getByTestId("det-2")).toBeInTheDocument();
    });

    it("Escape closes the drawer", async () => {
      const user = userEvent.setup();
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={renderDet}
        />,
      );
      await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
      await user.click(screen.getByRole("menuitem", { name: /view details/i }));
      expect(screen.getByRole("dialog")).toBeInTheDocument();
      await user.keyboard("{Escape}");
      expect(screen.queryByRole("dialog")).toBeNull();
    });

    it("clicking the row body still calls onRowOpen (page-owned open), without opening the drawer", async () => {
      const onRowOpen = vi.fn();
      const user = userEvent.setup();
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          onRowOpen={onRowOpen}
          renderDetails={renderDet}
        />,
      );
      await user.click(screen.getByText("beta"));
      expect(onRowOpen).toHaveBeenCalledWith(sample[1]);
      // Uncontrolled DataTable does not open the drawer on a bare row click —
      // that's the page's call (AlertsPage wires onRowOpen to open it).
      expect(screen.queryByRole("dialog")).toBeNull();
    });

    it("clicking the row body opens the drawer when the page supplies no onRowOpen", async () => {
      const user = userEvent.setup();
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={renderDet}
        />,
      );
      await user.click(screen.getByText("beta"));
      expect(screen.getByTestId("det-2")).toBeInTheDocument();
    });

    it("clicking a row's checkbox selects it without opening the drawer", async () => {
      const user = userEvent.setup();
      const onSelectionChange = vi.fn();
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          selectable
          selectedKeys={new Set<string>()}
          onSelectionChange={onSelectionChange}
          renderDetails={renderDet}
        />,
      );
      await user.click(screen.getByRole("checkbox", { name: /select row 2/i }));
      expect(onSelectionChange).toHaveBeenCalled();
      expect(screen.queryByRole("dialog")).toBeNull();
    });

    it("clicking a control inside a cell does not open the drawer", async () => {
      const user = userEvent.setup();
      const onCellButton = vi.fn();
      const withButton: ColumnDef<Row>[] = [
        {
          id: "name",
          header: "Name",
          cell: (r) => (
            <button type="button" onClick={onCellButton}>
              go {r.name}
            </button>
          ),
        },
      ];
      render(
        <DataTable
          data={sample}
          columns={withButton}
          rowKey={(r) => r.id}
          renderDetails={renderDet}
        />,
      );
      await user.click(screen.getByRole("button", { name: /go beta/i }));
      expect(onCellButton).toHaveBeenCalled();
      expect(screen.queryByRole("dialog")).toBeNull();
    });

    it("does not open the drawer on a row click that ends a text selection", async () => {
      const user = userEvent.setup();
      const sel = { isCollapsed: false, toString: () => "highlighted" } as unknown as Selection;
      const spy = vi.spyOn(window, "getSelection").mockReturnValue(sel);
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={renderDet}
        />,
      );
      await user.click(screen.getByText("beta"));
      expect(screen.queryByRole("dialog")).toBeNull();
      spy.mockRestore();
    });

    it("fires onDetailsKeyChange with the open key, and null on close (uncontrolled)", async () => {
      const onDetailsKeyChange = vi.fn();
      const user = userEvent.setup();
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={renderDet}
          onDetailsKeyChange={onDetailsKeyChange}
        />,
      );
      // Mount fires once with null (uncontrolled effect) — "nothing open".
      expect(onDetailsKeyChange.mock.calls).toEqual([[null]]);

      await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
      await user.click(screen.getByRole("menuitem", { name: /view details/i }));
      expect(onDetailsKeyChange).toHaveBeenLastCalledWith("1");

      await user.keyboard("{Escape}");
      expect(onDetailsKeyChange).toHaveBeenLastCalledWith(null);
    });

    it("controlled detailsKey renders exactly that row and routes opens through onDetailsKeyChange", async () => {
      const onDetailsKeyChange = vi.fn();
      const user = userEvent.setup();
      const { rerender } = render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={renderDet}
          detailsKey={null}
          onDetailsKeyChange={onDetailsKeyChange}
        />,
      );
      // Controlled + closed: no drawer, and the mount effect does NOT fire.
      expect(screen.queryByRole("dialog")).toBeNull();
      expect(onDetailsKeyChange).not.toHaveBeenCalled();

      // Opening a row asks the parent (write channel) without self-opening —
      // the modal drawer would otherwise make the table inert, so the open
      // affordance must round-trip through the controlled prop.
      await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
      await user.click(screen.getByRole("menuitem", { name: /view details/i }));
      expect(onDetailsKeyChange).toHaveBeenLastCalledWith("1");
      expect(screen.queryByRole("dialog")).toBeNull();

      // Parent applies detailsKey="1" → the drawer opens for exactly row 1.
      rerender(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={renderDet}
          detailsKey="1"
          onDetailsKeyChange={onDetailsKeyChange}
        />,
      );
      expect(screen.getByTestId("det-1")).toBeInTheDocument();

      // Switching the prop retargets to exactly that row.
      rerender(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={renderDet}
          detailsKey="2"
          onDetailsKeyChange={onDetailsKeyChange}
        />,
      );
      expect(screen.getByTestId("det-2")).toBeInTheDocument();
      expect(screen.queryByTestId("det-1")).toBeNull();
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

    it("e opens the details drawer for the focused row when renderDetails is set", () => {
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={(r) => <div data-testid={`exp-${r.id}`}>{r.name}</div>}
        />,
      );
      const table = screen.getByRole("grid");
      table.focus();
      fireEvent.keyDown(table, { key: "j" }); // focus row 1
      fireEvent.keyDown(table, { key: "e" });
      expect(screen.getByTestId("exp-1")).toBeInTheDocument();
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

    it("names the focused row via aria-activedescendant", () => {
      render(<DataTable data={sample} columns={columns} rowKey={(r) => r.id} />);
      const table = screen.getByRole("grid");
      table.focus();
      expect(table).not.toHaveAttribute("aria-activedescendant");
      fireEvent.keyDown(table, { key: "j" });
      const focusedRow = screen.getByText("alpha").closest("tr");
      expect(table.getAttribute("aria-activedescendant")).toBe(focusedRow?.id);
      expect(focusedRow?.id).toBeTruthy();
    });

    it("Enter opens the details drawer when the table has one and no onRowOpen", () => {
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={(r) => <div data-testid={`det-${r.id}`}>{r.name}</div>}
        />,
      );
      const table = screen.getByRole("grid");
      table.focus();
      fireEvent.keyDown(table, { key: "j" });
      fireEvent.keyDown(table, { key: "Enter" });
      expect(screen.getByTestId("det-1")).toBeInTheDocument();
    });

    it("Space toggles the focused row's selection", () => {
      const onSelectionChange = vi.fn();
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
      const table = screen.getByRole("grid");
      table.focus();
      fireEvent.keyDown(table, { key: "j" });
      fireEvent.keyDown(table, { key: " " });
      expect(onSelectionChange).toHaveBeenCalledWith(new Set(["1"]));
    });

    it("x clears the selection, then Escape clears the focus", () => {
      const onSelectionChange = vi.fn();
      const { rerender } = render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          selectable
          selectedKeys={new Set(["1"])}
          onSelectionChange={onSelectionChange}
        />,
      );
      const table = screen.getByRole("grid");
      table.focus();
      fireEvent.keyDown(table, { key: "j" });
      fireEvent.keyDown(table, { key: "x" });
      expect(onSelectionChange).toHaveBeenCalledWith(new Set());

      rerender(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          selectable
          selectedKeys={new Set()}
          onSelectionChange={onSelectionChange}
        />,
      );
      expect(screen.getByText("alpha").closest("tr")).toHaveAttribute("data-focused", "true");
      fireEvent.keyDown(table, { key: "Escape" });
      expect(screen.getByText("alpha").closest("tr")).not.toHaveAttribute("data-focused");
    });

    it("f toggles the inline row expansion, and the chevron does the same", async () => {
      const user = userEvent.setup();
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderRowExpansion={(r) => <div data-testid={`flow-${r.id}`}>{r.name} flow</div>}
          rowExpansionLabel="pipeline flow"
        />,
      );
      const table = screen.getByRole("grid");
      table.focus();
      fireEvent.keyDown(table, { key: "j" });
      fireEvent.keyDown(table, { key: "f" });
      expect(screen.getByTestId("flow-1")).toBeInTheDocument();
      fireEvent.keyDown(table, { key: "f" });
      expect(screen.queryByTestId("flow-1")).toBeNull();

      // Same state, reached with the mouse.
      await user.click(screen.getAllByRole("button", { name: /show pipeline flow/i })[0]!);
      expect(screen.getByTestId("flow-1")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /hide pipeline flow/i })).toHaveAttribute(
        "aria-expanded",
        "true",
      );
    });

    it("? opens the shortcuts legend", async () => {
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          keyboardHints={[{ keys: "A", label: "Acknowledge" }]}
        />,
      );
      const table = screen.getByRole("grid");
      table.focus();
      fireEvent.keyDown(table, { key: "?" });
      expect(await screen.findByText("Move between rows")).toBeInTheDocument();
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

    it("e does NOT open the drawer when Ctrl is held", () => {
      render(
        <DataTable
          data={sample}
          columns={columns}
          rowKey={(r) => r.id}
          renderDetails={(r) => <div data-testid={`exp-${r.id}`}>{r.name}</div>}
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
