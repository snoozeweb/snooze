import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { InjectAlertsDialog } from "./InjectAlertsDialog";

describe("InjectAlertsDialog", () => {
  it("does not render when closed", () => {
    render(<InjectAlertsDialog open={false} onOpenChange={() => undefined} />);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("opens on the REST API tab with the curl snippet and a docs link", () => {
    render(<InjectAlertsDialog open onOpenChange={() => undefined} />);
    expect(screen.getByRole("dialog", { name: /how to inject alerts/i })).toBeInTheDocument();
    // The endpoint line (exact) and the curl snippet (substring) are distinct
    // elements — assert each specifically so neither query matches both.
    expect(screen.getByText("POST /api/v1/alerts")).toBeInTheDocument();
    expect(screen.getByText(/curl -s -X POST/)).toBeInTheDocument();
    const restDocs = screen.getByRole("link", { name: /full rest api docs/i });
    expect(restDocs).toHaveAttribute(
      "href",
      "https://snoozeweb.github.io/snooze/general/integrations/rest-api",
    );
  });

  it("the Webhooks tab shows the picked receiver's URL and switches sources", async () => {
    const user = userEvent.setup();
    render(<InjectAlertsDialog open onOpenChange={() => undefined} />);
    await user.click(screen.getByRole("tab", { name: /webhooks/i }));
    // Grafana is the default selection. Anchor on the full snippet URL so the
    // endpoint line ("POST /api/v1/webhook/grafana") doesn't also match.
    expect(screen.getByText(/^https?:\/\/.+\/api\/v1\/webhook\/grafana$/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Grafana$/ })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await user.click(screen.getByRole("button", { name: /^Datadog$/ }));
    expect(screen.getByText(/^https?:\/\/.+\/api\/v1\/webhook\/datadog$/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Datadog$/ })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("the Daemon inputs tab shows a daemon source", async () => {
    const user = userEvent.setup();
    render(<InjectAlertsDialog open onOpenChange={() => undefined} />);
    await user.click(screen.getByRole("tab", { name: /daemon inputs/i }));
    // Syslog is the default selection in the daemon family.
    expect(screen.getByText(/rsyslog/i)).toBeInTheDocument();
  });

  it("the footer links to the quickstart and Close fires onOpenChange", async () => {
    const onOpenChange = vi.fn();
    const user = userEvent.setup();
    render(<InjectAlertsDialog open onOpenChange={onOpenChange} />);
    expect(screen.getByRole("link", { name: /full guide/i })).toHaveAttribute(
      "href",
      "https://snoozeweb.github.io/snooze/general/integrations/sending-alerts",
    );
    await user.click(screen.getByRole("button", { name: /^close$/i }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});
