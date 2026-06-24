import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { Metadata } from "@/shared/forms/types";
import { IntegrationModeChooser } from "./IntegrationModeChooser";

const JIRA: Metadata = {
  plugin_name: "jira",
  name: "Create a JIRA issue",
  category: "ticketing",
  daemon: { name: "snooze-jira", blurb: "Auto-close records.", doc_url: "https://docs/jira#daemon" },
};

describe("IntegrationModeChooser", () => {
  it("renders both options with the daemon docs link", () => {
    render(<IntegrationModeChooser plugin={JIRA} onUseBuiltin={() => undefined} onBack={() => undefined} />);
    expect(screen.getByText("Built-in")).toBeTruthy();
    const link = screen.getByRole("link", { name: /Advanced · snooze-jira/ });
    expect(link.getAttribute("href")).toBe("https://docs/jira#daemon");
    expect(link.getAttribute("target")).toBe("_blank");
  });

  it("calls onUseBuiltin when Built-in is clicked", async () => {
    const onUseBuiltin = vi.fn();
    const user = userEvent.setup();
    render(<IntegrationModeChooser plugin={JIRA} onUseBuiltin={onUseBuiltin} onBack={() => undefined} />);
    await user.click(screen.getByText("Built-in"));
    expect(onUseBuiltin).toHaveBeenCalledOnce();
  });

  it("calls onBack when Back is clicked", async () => {
    const onBack = vi.fn();
    const user = userEvent.setup();
    render(<IntegrationModeChooser plugin={JIRA} onUseBuiltin={() => undefined} onBack={onBack} />);
    await user.click(screen.getByText(/Back/));
    expect(onBack).toHaveBeenCalledOnce();
  });
});
