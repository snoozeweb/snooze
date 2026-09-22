import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "./Tooltip";
import { RowDetailsDrawer } from "./RowDetailsDrawer";

type Row = { id: string; label: string };

const ROWS: Row[] = [
  { id: "a", label: "Alpha" },
  { id: "b", label: "Bravo" },
  { id: "c", label: "Charlie" },
];

/**
 * A details subtree with state of its own — the shape every real one has (the
 * alert inspector's tab selection, the analysis editor's form). If the drawer
 * swaps `row` without remounting, whatever was typed here survives onto the
 * next row, which is how a draft written against Alpha gets saved onto Bravo.
 */
function DraftProbe({ row }: { row: Row }) {
  const [text, setText] = useState("");
  return (
    <div>
      <span data-testid="probe-row">{row.label}</span>
      <input aria-label="draft" value={text} onChange={(e) => setText(e.target.value)} />
    </div>
  );
}

function renderDrawer(props: {
  rows: Row[];
  activeKey: string | null;
  onNavigate?: (index: number) => void;
  onClose?: () => void;
}) {
  const view = render(
    <TooltipProvider>
      <RowDetailsDrawer<Row>
        rows={props.rows}
        rowKey={(r) => r.id}
        activeKey={props.activeKey}
        onNavigate={props.onNavigate ?? (() => undefined)}
        onClose={props.onClose ?? (() => undefined)}
        renderDetails={(row) => <DraftProbe row={row} />}
        detailsTitle={(row) => row.label}
      />
    </TooltipProvider>,
  );
  const rerender = (next: { rows?: Row[]; activeKey?: string | null }) =>
    view.rerender(
      <TooltipProvider>
        <RowDetailsDrawer<Row>
          rows={next.rows ?? props.rows}
          rowKey={(r) => r.id}
          activeKey={next.activeKey !== undefined ? next.activeKey : props.activeKey}
          onNavigate={props.onNavigate ?? (() => undefined)}
          onClose={props.onClose ?? (() => undefined)}
          renderDetails={(row) => <DraftProbe row={row} />}
          detailsTitle={(row) => row.label}
        />
      </TooltipProvider>,
    );
  return { ...view, rerender };
}

describe("RowDetailsDrawer", () => {
  it("remounts the details subtree when the drawer retargets to another row", async () => {
    const user = userEvent.setup();
    const { rerender } = renderDrawer({ rows: ROWS, activeKey: "a" });

    await user.type(screen.getByLabelText("draft"), "hello");
    expect(screen.getByLabelText("draft")).toHaveValue("hello");

    // Prev/next (chevrons, J/K) retarget the drawer by changing activeKey.
    rerender({ activeKey: "b" });

    expect(screen.getByTestId("probe-row")).toHaveTextContent("Bravo");
    // The draft belonged to Alpha. Carrying it onto Bravo is how a PUT lands
    // on the wrong alert.
    expect(screen.getByLabelText("draft")).toHaveValue("");
  });

  it("keeps rendering the open row after it leaves the rows list", async () => {
    const user = userEvent.setup();
    const { rerender } = renderDrawer({ rows: ROWS, activeKey: "a" });
    await user.type(screen.getByLabelText("draft"), "hello");

    // A poll (or a filter change) drops the open row from the page. The drawer
    // must not tear down the subtree the operator is typing into.
    rerender({ rows: [ROWS[1]!, ROWS[2]!] });

    expect(screen.getByTestId("probe-row")).toHaveTextContent("Alpha");
    expect(screen.getByLabelText("draft")).toHaveValue("hello");
  });

  it("renders nothing once activeKey is cleared", () => {
    const { rerender } = renderDrawer({ rows: ROWS, activeKey: "a" });
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    rerender({ activeKey: null });
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("disables prev/next while the open row is off the page", () => {
    const onNavigate = vi.fn();
    const { rerender } = renderDrawer({ rows: ROWS, activeKey: "a", onNavigate });
    rerender({ rows: [ROWS[1]!, ROWS[2]!] });

    // Position is unknowable, so the counter says so and the chevrons are inert
    // rather than paging from a bogus index.
    expect(screen.getByRole("button", { name: /Previous row/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: /Next row/ })).toBeDisabled();
    expect(onNavigate).not.toHaveBeenCalled();
  });
});
