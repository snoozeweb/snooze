import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Code, CodeBlock } from "./Code";

describe("Code", () => {
  it("renders inline code", () => {
    render(<Code>foo</Code>);
    const el = screen.getByText("foo");
    expect(el.tagName).toBe("CODE");
  });
});

describe("CodeBlock", () => {
  it("renders the content inside <pre><code>", () => {
    const { container } = render(<CodeBlock>{"line 1\nline 2"}</CodeBlock>);
    const pre = container.querySelector("pre");
    expect(pre).not.toBeNull();
    expect(pre!.textContent).toContain("line 1");
  });

  it("shows a copy button when copyable is true", () => {
    render(<CodeBlock copyable>{"x"}</CodeBlock>);
    expect(screen.getByRole("button", { name: /copy/i })).toBeInTheDocument();
  });

  it("copies to clipboard on click", async () => {
    const user = userEvent.setup();
    const writeText = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue(undefined);
    render(<CodeBlock copyable>{"payload"}</CodeBlock>);
    await user.click(screen.getByRole("button", { name: /copy/i }));
    expect(writeText).toHaveBeenCalledWith("payload");
  });
});
