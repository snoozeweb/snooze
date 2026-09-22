import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ProvenanceLine, type AnalysisMeta } from "./ProvenanceLine";
import { analysisEpoch } from "./time";

function renderLine(analysis: AnalysisMeta | undefined): HTMLElement {
  function Wrapper({ children }: { children: ReactNode }) {
    return <TooltipProvider>{children}</TooltipProvider>;
  }
  const { container } = render(<ProvenanceLine analysis={analysis} />, { wrapper: Wrapper });
  return container;
}

describe("analysisEpoch", () => {
  it("converts an RFC3339 stamp to epoch seconds", () => {
    expect(analysisEpoch("2026-09-21T10:00:00Z")).toBe(
      Math.floor(Date.UTC(2026, 8, 21, 10) / 1000),
    );
  });

  it("rejects Go's zero time rather than reporting 739879 days ago", () => {
    // A subtree written by something that marshalled a bare time.Time carries
    // 0001-01-01T00:00:00Z. It parses, so only a sanity floor catches it.
    expect(analysisEpoch("0001-01-01T00:00:00Z")).toBeUndefined();
  });

  it("rejects a bare year, which Date.parse happily reads as January 1st", () => {
    expect(analysisEpoch("2026")).toBeUndefined();
  });

  it("rejects a stamp more than a day in the future", () => {
    const soon = new Date(Date.now() + 36 * 3600 * 1000).toISOString();
    expect(analysisEpoch(soon)).toBeUndefined();
  });

  it("allows the small clock skew a cluster actually has", () => {
    const skewed = new Date(Date.now() + 60 * 1000).toISOString();
    expect(analysisEpoch(skewed)).not.toBeUndefined();
  });

  it("rejects an unparseable or absent stamp", () => {
    expect(analysisEpoch("not a date")).toBeUndefined();
    expect(analysisEpoch(undefined)).toBeUndefined();
    expect(analysisEpoch("")).toBeUndefined();
    expect(analysisEpoch(5)).toBeUndefined();
  });
});

describe("ProvenanceLine", () => {
  it("renders the tool, the subject and the time", () => {
    const container = renderLine({
      at: new Date(Date.now() - 120_000).toISOString(),
      by: "agent-bot",
      source: "alert-rca",
    });
    expect(screen.getByText("alert-rca")).toBeInTheDocument();
    expect(screen.getByText("agent-bot")).toBeInTheDocument();
    expect(container.querySelector("time")).not.toBeNull();
  });

  it("renders nothing when the whole block is absent", () => {
    const container = renderLine(undefined);
    expect(container).toBeEmptyDOMElement();
  });

  it("keeps the author but drops a nonsense timestamp", () => {
    const container = renderLine({ at: "0001-01-01T00:00:00Z", by: "agent-bot" });
    expect(screen.getByText("agent-bot")).toBeInTheDocument();
    expect(container.querySelector("time")).toBeNull();
  });

  it("does not collide React keys when the tool and the subject are the same name", () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => undefined);
    try {
      renderLine({ at: "", by: "snooze-web", source: "snooze-web" });
      expect(screen.getAllByText("snooze-web")).toHaveLength(2);
      const keyWarnings = spy.mock.calls.filter((args) =>
        args.some((a) => typeof a === "string" && a.includes("same key")),
      );
      expect(keyWarnings).toEqual([]);
    } finally {
      spy.mockRestore();
    }
  });
});
