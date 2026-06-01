import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { Metadata } from "@/shared/forms/types";
import { IntegrationGallery } from "./IntegrationGallery";

const PLUGINS: Metadata[] = [
  {
    plugin_name: "teams",
    name: "Microsoft Teams",
    display_name: "Post a card to a Teams channel",
    category: "chat",
    action_form: { webhook_url: { display_name: "Webhook URL", component: "String" } },
  },
  {
    plugin_name: "slack",
    name: "Slack",
    display_name: "Post to a Slack channel",
    category: "chat",
    action_form: { webhook_url: { display_name: "Webhook URL", component: "String" } },
  },
  {
    plugin_name: "pagerduty",
    name: "PagerDuty",
    display_name: "Trigger an incident",
    category: "oncall",
    action_form: { routing_key: { display_name: "Routing key", component: "String" } },
  },
  {
    plugin_name: "webhook",
    name: "Webhook",
    display_name: "POST to any URL",
    category: "generic",
    action_form: { url: { display_name: "URL", component: "String" } },
  },
  {
    plugin_name: "mystery",
    name: "Mystery",
    display_name: "No category set",
    action_form: { x: { display_name: "X", component: "String" } },
  },
];

describe("IntegrationGallery", () => {
  it("renders only non-empty category groups, in fixed order", () => {
    render(<IntegrationGallery plugins={PLUGINS} onPick={() => undefined} />);
    const headings = screen.getAllByRole("heading").map((h) => h.textContent);
    expect(headings).toEqual(["Chat", "On-call / Incident", "Generic"]);
  });

  it("places an uncategorised plugin in the Generic bucket", () => {
    render(<IntegrationGallery plugins={PLUGINS} onPick={() => undefined} />);
    expect(screen.getByRole("button", { name: /Mystery/ })).toBeTruthy();
  });

  it("calls onPick with the plugin_name when a card is clicked", async () => {
    const onPick = vi.fn();
    const user = userEvent.setup();
    render(<IntegrationGallery plugins={PLUGINS} onPick={onPick} />);
    await user.click(screen.getByRole("button", { name: /Microsoft Teams/ }));
    expect(onPick).toHaveBeenCalledWith("teams");
  });
});
