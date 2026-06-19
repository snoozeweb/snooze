import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { DataTableContextMenu, type ContextMenuItem } from "./DataTableContextMenu";

const baseItems: ContextMenuItem[] = [
  { key: "delete", label: "Delete", danger: true, onSelect: vi.fn() },
];

describe("DataTableContextMenu", () => {
  it("renders the supplied items and no Copy item by default", () => {
    render(<DataTableContextMenu items={baseItems} x={0} y={0} onClose={() => undefined} />);
    expect(screen.getByRole("menuitem", { name: "Delete" })).toBeInTheDocument();
    expect(screen.queryByText("Copy")).toBeNull();
  });

  it("omits the Copy item when copyText is blank/whitespace", () => {
    render(
      <DataTableContextMenu
        items={baseItems}
        x={0}
        y={0}
        copyText="   "
        onClose={() => undefined}
      />,
    );
    expect(screen.queryByText("Copy")).toBeNull();
  });

  it("prepends a Copy item at the very top when text is selected", () => {
    render(
      <DataTableContextMenu
        items={baseItems}
        x={0}
        y={0}
        copyText="hello world"
        onClose={() => undefined}
      />,
    );
    const items = screen.getAllByRole("menuitem");
    expect(items.length).toBe(2);
    expect(items[0]).toHaveTextContent("Copy");
    expect(items[1]).toHaveTextContent("Delete");
  });

  it("Copy writes exactly the selected text to the clipboard and closes the menu", async () => {
    const onClose = vi.fn();
    // userEvent.setup() installs navigator.clipboard, so spy on it afterwards
    // (mirrors Code.test / JsonViewer.test).
    const user = userEvent.setup();
    const writeText = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue(undefined);
    render(
      <DataTableContextMenu items={baseItems} x={0} y={0} copyText="grab me" onClose={onClose} />,
    );
    await user.click(screen.getByText("Copy"));
    expect(writeText).toHaveBeenCalledWith("grab me");
    expect(onClose).toHaveBeenCalled();
    writeText.mockRestore();
  });
});
