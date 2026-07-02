import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { DaemonCompanionNote } from "./DaemonCompanionNote";

const DAEMON = {
  name: "snooze-jira",
  blurb: "Auto-close records when the linked ticket is resolved.",
  doc_url: "https://docs/jira#daemon",
};

describe("DaemonCompanionNote", () => {
  it("frames the daemon as optional and non-blocking, linking to its docs", () => {
    render(<DaemonCompanionNote daemon={DAEMON} />);
    // The blurb is shown so the operator knows what the companion adds…
    expect(screen.getByText(/auto-close records/i)).toBeInTheDocument();
    // …and it is explicitly marked not required (the action delivers on its own).
    expect(screen.getByText(/not required/i)).toBeInTheDocument();
    const link = screen.getByRole("link", { name: /set up snooze-jira/i });
    expect(link).toHaveAttribute("href", "https://docs/jira#daemon");
    expect(link).toHaveAttribute("target", "_blank");
  });
});
