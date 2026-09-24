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
    const writeText = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue(undefined);
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

  describe("search", () => {
    const value = { host: "srv-db1", labels: { team: "db", cluster: "prod-db" }, count: 3 };

    it("has no search box unless asked for", () => {
      render(<JsonViewer value={value} />);
      expect(screen.queryByRole("searchbox")).toBeNull();
    });

    it("highlights every match, case-insensitively, in keys and values", async () => {
      const user = userEvent.setup();
      const { container } = render(<JsonViewer value={value} searchable />);
      await user.type(screen.getByRole("searchbox", { name: /find in/i }), "DB");
      const marks = [...container.querySelectorAll("mark")].map((m) => m.textContent);
      expect(marks).toEqual(["db", "db", "db"]);
      expect(screen.getByRole("status")).toHaveTextContent("1 of 3");
    });

    it("steps through matches with Enter and Shift+Enter, wrapping", async () => {
      const user = userEvent.setup();
      const { container } = render(<JsonViewer value={value} searchable />);
      const box = screen.getByRole("searchbox", { name: /find in/i });
      await user.type(box, "db");
      const current = () =>
        [...container.querySelectorAll("mark")].findIndex((m) => m.hasAttribute("data-current"));
      expect(current()).toBe(0);
      await user.keyboard("{Enter}");
      expect(current()).toBe(1);
      expect(screen.getByRole("status")).toHaveTextContent("2 of 3");
      await user.keyboard("{Shift>}{Enter}{/Shift}{Shift>}{Enter}{/Shift}");
      expect(current()).toBe(2);
      await user.click(screen.getByRole("button", { name: /next match/i }));
      expect(current()).toBe(0);
    });

    it("searches inside a collapsed key", async () => {
      const user = userEvent.setup();
      render(<JsonViewer value={value} searchable />);
      await user.click(screen.getByRole("button", { name: /toggle labels/i }));
      expect(screen.queryByText(/prod-/)).toBeNull();
      await user.type(screen.getByRole("searchbox", { name: /find in/i }), "prod");
      expect(screen.getByText("prod")).toBeInTheDocument();
    });

    it("says so when nothing matches", async () => {
      const user = userEvent.setup();
      const { container } = render(<JsonViewer value={value} searchable />);
      await user.type(screen.getByRole("searchbox", { name: /find in/i }), "nope");
      expect(container.querySelector("mark")).toBeNull();
      expect(screen.getByRole("status")).toHaveTextContent("No matches");
      expect(screen.getByRole("button", { name: /next match/i })).toBeDisabled();
    });
  });
});
