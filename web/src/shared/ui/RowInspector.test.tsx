import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { RowInspector } from "./RowInspector";

describe("RowInspector", () => {
  it("renders the title and children in a labelled complementary region", () => {
    render(
      <RowInspector title="srv-1" onClose={vi.fn()}>
        <div>inspector body</div>
      </RowInspector>,
    );
    expect(screen.getByRole("complementary", { name: /row inspector/i })).toBeInTheDocument();
    expect(screen.getByText("srv-1")).toBeInTheDocument();
    expect(screen.getByText("inspector body")).toBeInTheDocument();
  });

  it("renders the position counter as 1-based N / M", () => {
    render(
      <RowInspector title="t" position={{ index: 2, total: 5 }} onClose={vi.fn()}>
        <div />
      </RowInspector>,
    );
    expect(screen.getByText("3 / 5")).toBeInTheDocument();
  });

  it("calls onClose when the close button is clicked", async () => {
    const onClose = vi.fn();
    const user = userEvent.setup();
    render(
      <RowInspector title="t" onClose={onClose}>
        <div />
      </RowInspector>,
    );
    await user.click(screen.getByRole("button", { name: /close inspector/i }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("closes on Escape", () => {
    const onClose = vi.fn();
    render(
      <RowInspector title="t" onClose={onClose}>
        <div />
      </RowInspector>,
    );
    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("disables prev/next when their handlers are undefined and fires them otherwise", async () => {
    const onPrev = vi.fn();
    const user = userEvent.setup();
    const { rerender } = render(
      <RowInspector title="t" onClose={vi.fn()}>
        <div />
      </RowInspector>,
    );
    // No handlers → both nav buttons disabled.
    expect(screen.getByRole("button", { name: /previous row/i })).toBeDisabled();
    expect(screen.getByRole("button", { name: /next row/i })).toBeDisabled();

    rerender(
      <RowInspector title="t" onPrev={onPrev} onClose={vi.fn()}>
        <div />
      </RowInspector>,
    );
    const prev = screen.getByRole("button", { name: /previous row/i });
    expect(prev).toBeEnabled();
    await user.click(prev);
    expect(onPrev).toHaveBeenCalledTimes(1);
  });

  it("exposes a keyboard-operable resize separator", () => {
    render(
      <RowInspector title="t" onClose={vi.fn()}>
        <div />
      </RowInspector>,
    );
    const separator = screen.getByRole("separator", { name: /resize inspector/i });
    expect(separator).toHaveAttribute("aria-orientation", "vertical");
    expect(separator).toHaveAttribute("tabindex", "0");
    const before = Number(separator.getAttribute("aria-valuenow"));
    // ArrowLeft grows the panel (docked right), so aria-valuenow increases.
    fireEvent.keyDown(separator, { key: "ArrowLeft" });
    const after = Number(separator.getAttribute("aria-valuenow"));
    expect(after).toBeGreaterThan(before);
  });
});
