// web/src/shared/ui/Diff.test.tsx
import { render, screen, getDefaultNormalizer } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { Diff } from "./Diff";

const noTrim = { normalizer: getDefaultNormalizer({ trim: false, collapseWhitespace: false }) };

describe("Diff", () => {
  it("renders added and removed lines", () => {
    render(<Diff oldText={"a\nb\nc\n"} newText={"a\nB\nc\n"} />);
    expect(screen.getByText(/^- b$/)).toBeInTheDocument();
    expect(screen.getByText(/^\+ B$/)).toBeInTheDocument();
  });
  it("shows 'No changes' when texts are equal", () => {
    render(<Diff oldText={"a\nb\n"} newText={"a\nb\n"} />);
    expect(screen.getByText(/no changes/i)).toBeInTheDocument();
  });
  it("shows the unchanged context line", () => {
    render(<Diff oldText={"a\nb\n"} newText={"a\nB\n"} />);
    expect(screen.getByText(/^ {2}a$/, noTrim)).toBeInTheDocument();
  });
});
