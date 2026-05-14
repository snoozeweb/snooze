// web/src/shared/ui/DiffSection.test.tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect } from "vitest";
import { DiffSection } from "./DiffSection";

describe("DiffSection", () => {
  it("hides when original is undefined", () => {
    render(<DiffSection original={undefined} current={{ a: 1 }} />);
    expect(screen.queryByRole("button", { name: /diff/i })).not.toBeInTheDocument();
  });
  it("renders a collapsed Diff toggle when original is defined", async () => {
    const user = userEvent.setup();
    render(<DiffSection original={{ a: 1 }} current={{ a: 2 }} />);
    const btn = screen.getByRole("button", { name: /diff/i });
    expect(btn).toBeInTheDocument();
    expect(screen.queryByLabelText(/^Diff$/)).not.toBeInTheDocument();
    await user.click(btn);
    expect(screen.getByLabelText(/^Diff$/)).toBeInTheDocument();
  });
});
