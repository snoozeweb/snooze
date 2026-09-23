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
  it("renders the summary, the scope and the confidence meter", () => {
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

  it("sets a short summary as the headline, with no body under it", () => {
    render(<RootCauseView rootCause={rootCause()} />);
    expect(
      screen.getByRole("heading", { name: "systemd-journald filled /var" }),
    ).toBeInTheDocument();
    expect(screen.queryByTestId("verdict-body")).toBeNull();
  });

  it("promotes the first sentence of an over-long summary and keeps the rest as the body", () => {
    // The prod shape: a 430-character "one sentence" with no detail. Setting
    // all of it in headline type made a seven-line paragraph the loudest
    // thing in the pane.
    const first =
      "The kopia maintenance job on ovh/velero failed because the repository lock was held by a stale pod.";
    const rest =
      "The pod was evicted during the node drain at 02:10 and never released the lock, so every run since has exited early with the same error while the backups themselves kept succeeding, which is why no restore test has flagged it yet and why it only surfaced as this alert.";
    render(<RootCauseView rootCause={rootCause({ summary: `${first} ${rest}` })} />);
    expect(screen.getByRole("heading", { name: first })).toBeInTheDocument();
    expect(screen.getByTestId("verdict-body")).toHaveTextContent(rest);
  });

  it("reads `detail` as the body when the analysis carries one", () => {
    render(
      <RootCauseView
        rootCause={rootCause({ detail: "The journal grew 4.2G in six hours after the upgrade." })}
      />,
    );
    expect(
      screen.getByRole("heading", { name: "systemd-journald filled /var" }),
    ).toBeInTheDocument();
    expect(screen.getByTestId("verdict-body")).toHaveTextContent(
      "The journal grew 4.2G in six hours after the upgrade.",
    );
  });

  it("leads with the plan's verdict when one is stated", () => {
    render(<RootCauseView rootCause={rootCause()} status="self_resolved" />);
    expect(screen.getByText("Self-resolved")).toBeInTheDocument();
  });

  it("leaves the evidence to its own section", () => {
    render(
      <RootCauseView rootCause={rootCause({ evidence: ["journalctl: 4.2G under /var/log"] })} />,
    );
    expect(screen.queryByText(/4.2G under/)).toBeNull();
  });
});
