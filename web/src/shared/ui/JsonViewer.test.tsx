import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { JsonViewer } from "./JsonViewer";

describe("JsonViewer", () => {
  it("renders nested objects with 2-space indentation", () => {
    const value = { a: 1, b: { c: "x" } };
    const { container } = render(<JsonViewer value={value} />);
    const text = container.textContent ?? "";
    expect(text).toContain('"a": 1');
    expect(text).toContain('"b"');
    // Nested key indented by 4 spaces (2 levels × 2 spaces).
    expect(text).toContain('    "c": "x"');
  });

  it("copies the stringified JSON when the copy button is clicked", async () => {
    const value = { a: 1 };
    // userEvent.setup() installs a clipboard polyfill on navigator; spy on
    // its writeText so we capture the same instance the component sees.
    const user = userEvent.setup();
    const writeText = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue(undefined);
    render(<JsonViewer value={value} />);
    await user.click(screen.getByRole("button", { name: /copy/i }));
    expect(writeText).toHaveBeenCalledWith(JSON.stringify(value, null, 2));
  });

  it("collapses a top-level key when its chevron is clicked", async () => {
    const value = { outer: { inner: "secret" }, other: 42 };
    const user = userEvent.setup();
    render(<JsonViewer value={value} />);
    // Inner content is visible before collapse.
    expect(screen.getByText(/"secret"/)).toBeInTheDocument();
    const toggles = screen.getAllByRole("button", { name: /toggle outer/i });
    await user.click(toggles[0]!);
    expect(screen.queryByText(/"secret"/)).toBeNull();
    // Sibling top-level key still visible.
    expect(screen.getByText(/"other"/)).toBeInTheDocument();
  });

  it("does not render top-level chevrons for primitive values", () => {
    const value = { a: 1, b: "two", c: null };
    render(<JsonViewer value={value} />);
    expect(screen.queryByRole("button", { name: /toggle a/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /toggle b/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /toggle c/i })).toBeNull();
  });
});
