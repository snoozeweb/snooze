import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AlertFlowChart } from "./AlertFlowChart";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import type { Record_ } from "./types";

// Non-error action chips render their hint through the shared Tooltip, which
// needs a TooltipProvider ancestor (mounted app-wide in app/router.tsx).
function renderChart(row: Record_) {
  return render(
    <TooltipProvider delay={0}>
      <AlertFlowChart row={row} />
    </TooltipProvider>,
  );
}

const base: Record_ = {
  uid: "u1",
  source: "syslog",
  rules: ["disk-warn", "env-tag"],
  aggregate: "Host and Message",
  hash: "abcdef0123456789",
};

describe("AlertFlowChart", () => {
  it("renders the input, rules and aggregate, then notifications and actions", () => {
    renderChart({
      ...base,
      notifications: ["oncall"],
      actions: [
        { name: "email", notification: "oncall", status: "success" },
        { name: "webhook", notification: "oncall", status: "error", error: "dial tcp timeout" },
      ],
    });
    expect(screen.getByText("syslog")).toBeInTheDocument();
    expect(screen.getByText("disk-warn, env-tag")).toBeInTheDocument();
    expect(screen.getByText("oncall")).toBeInTheDocument();
    expect(screen.getByText(/email/)).toBeInTheDocument();
    expect(screen.getByText(/webhook/)).toBeInTheDocument();
  });

  it("is terminal at the snooze node when snoozed", () => {
    renderChart({ ...base, snoozed: "maint-window", notifications: ["oncall"] });
    expect(screen.getByText("maint-window")).toBeInTheDocument();
    expect(screen.queryByText("Notifications")).not.toBeInTheDocument();
    expect(screen.queryByText("oncall")).not.toBeInTheDocument();
  });

  it("reveals the error message when an error chip is clicked", async () => {
    renderChart({
      ...base,
      notifications: ["oncall"],
      actions: [{ name: "webhook", status: "error", error: "dial tcp timeout" }],
    });
    await userEvent.click(screen.getByRole("button", { name: /webhook error details/i }));
    expect(await screen.findByText("dial tcp timeout")).toBeInTheDocument();
  });

  it("renders non-error action chips with no error-details popover", () => {
    renderChart({
      ...base,
      notifications: ["oncall"],
      actions: [
        { name: "page", status: "skipped" },
        { name: "email", status: "pending" },
        { name: "webhook", status: "sent" },
      ],
    });
    expect(screen.getByText(/page/)).toBeInTheDocument();
    expect(screen.getByText(/email/)).toBeInTheDocument();
    expect(screen.getByText(/webhook/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /error details/i })).not.toBeInTheDocument();
  });

  it("shows the none placeholder for empty rules and actions on a minimal record", () => {
    renderChart({ uid: "u2", source: "prom" });
    // Empty rules, notifications and actions each render the placeholder.
    expect(screen.getAllByText("none").length).toBeGreaterThanOrEqual(2);
    // Scope the meaningful checks to the Rules and Actions nodes specifically.
    const node = (label: string) => screen.getByText(label).closest("div")!.parentElement!;
    expect(node("Rules")).toHaveTextContent("none");
    expect(node("Actions")).toHaveTextContent("none");
  });
});
