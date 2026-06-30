import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AlertFlowChart } from "./AlertFlowChart";
import type { Record_ } from "./types";

const base: Record_ = {
  uid: "u1",
  source: "syslog",
  rules: ["disk-warn", "env-tag"],
  aggregate: "Host and Message",
  hash: "abcdef0123456789",
};

describe("AlertFlowChart", () => {
  it("renders the input, rules and aggregate, then notifications and actions", () => {
    render(
      <AlertFlowChart
        row={{
          ...base,
          notifications: ["oncall"],
          actions: [
            { name: "email", notification: "oncall", status: "success" },
            { name: "webhook", notification: "oncall", status: "error", error: "dial tcp timeout" },
          ],
        }}
      />,
    );
    expect(screen.getByText("syslog")).toBeInTheDocument();
    expect(screen.getByText("disk-warn, env-tag")).toBeInTheDocument();
    expect(screen.getByText("oncall")).toBeInTheDocument();
    expect(screen.getByText(/email/)).toBeInTheDocument();
    expect(screen.getByText(/webhook/)).toBeInTheDocument();
  });

  it("is terminal at the snooze node when snoozed", () => {
    render(<AlertFlowChart row={{ ...base, snoozed: "maint-window", notifications: ["oncall"] }} />);
    expect(screen.getByText("maint-window")).toBeInTheDocument();
    expect(screen.queryByText("Notifications")).not.toBeInTheDocument();
    expect(screen.queryByText("oncall")).not.toBeInTheDocument();
  });

  it("reveals the error message when an error chip is clicked", async () => {
    render(
      <AlertFlowChart
        row={{ ...base, notifications: ["oncall"], actions: [{ name: "webhook", status: "error", error: "dial tcp timeout" }] }}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: /webhook error details/i }));
    expect(await screen.findByText("dial tcp timeout")).toBeInTheDocument();
  });
});
