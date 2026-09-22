import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { RootCauseView, type AgenticRootCause } from "./RootCauseView";

function rootCause(over: Partial<AgenticRootCause> = {}): AgenticRootCause {
  return {
    summary: "systemd-journald filled /var",
    scope: "srv-victoria1:/var",
    confidence: "medium",
    ...over,
  } as AgenticRootCause;
}

describe("RootCauseView", () => {
  it("renders the summary, the scope and the confidence chip", () => {
    render(<RootCauseView rootCause={rootCause()} />);
    expect(screen.getByText("systemd-journald filled /var")).toBeInTheDocument();
    expect(screen.getByText("srv-victoria1:/var")).toBeInTheDocument();
    expect(screen.getByText("Medium confidence")).toBeInTheDocument();
  });

  it("omits the chip for a confidence outside the closed enum, like AnalysedDot", () => {
    // A subtree hand-edited in the DB, or written before a level was retired.
    // Painting it with `variant={undefined}` would show an unlabelled chip.
    render(<RootCauseView rootCause={rootCause({ confidence: "certain" as never })} />);
    expect(screen.queryByText(/confidence/)).toBeNull();
    expect(screen.getByText("srv-victoria1:/var")).toBeInTheDocument();
  });

  it("renders no meta row at all when there is neither a scope nor a usable confidence", () => {
    const { container } = render(
      <RootCauseView rootCause={rootCause({ scope: "", confidence: "certain" as never })} />,
    );
    expect(container.querySelectorAll("span[class*='badge']")).toHaveLength(0);
  });

  it("honours showConfidence=false even for a valid level", () => {
    render(<RootCauseView rootCause={rootCause()} showConfidence={false} />);
    expect(screen.queryByText("Medium confidence")).toBeNull();
  });
});
