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
    plugin_name: "jira",
    name: "Create a JIRA issue",
    display_name: "Open a JIRA Cloud issue",
    category: "ticketing",
    action_form: { project_key: { display_name: "Project Key", component: "String" } },
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
    expect(headings).toEqual(["Generic", "Chat", "On-call / Incident", "Ticketing"]);
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

  it("shows the brand glyph for a branded notifier", () => {
    render(<IntegrationGallery plugins={PLUGINS} onPick={() => undefined} />);
    const slack = screen.getByRole("button", { name: /Slack/ });
    expect(slack.querySelector("use")?.getAttribute("href")).toBe("/web/brands.svg#brand-slack");
    const pagerduty = screen.getByRole("button", { name: /PagerDuty/ });
    expect(pagerduty.querySelector("use")?.getAttribute("href")).toBe(
      "/web/brands.svg#brand-pagerduty",
    );
  });

  it("shows the brand glyph for the Jira card", () => {
    render(<IntegrationGallery plugins={PLUGINS} onPick={() => undefined} />);
    const jira = screen.getByRole("button", { name: /Create a JIRA issue/ });
    expect(jira.querySelector("use")?.getAttribute("href")).toBe("/web/brands.svg#brand-jira");
  });

  it("falls back to the category glyph for a notifier without a brand logo", () => {
    render(<IntegrationGallery plugins={PLUGINS} onPick={() => undefined} />);
    // webhook is generic and has no vendored brand mark.
    const webhook = screen.getByRole("button", { name: /Webhook/ });
    expect(webhook.querySelector("use")?.getAttribute("href")).toBe("/web/icons.svg#icon-plug");
  });
});
