import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { actionColumns, notificationColumns } from "./columns";
import type { Action, Notification } from "./types";

function actionCell(sub: Record<string, unknown>) {
  const col = actionColumns.find((c) => c.id === "action")!;
  return col.cell({ name: "x", action: { selected: "webhook", subcontent: sub } } as Action);
}

describe("actions summary column", () => {
  it("summarizes a webhook via webhook_url (previously blank for most integrations)", () => {
    render(<>{actionCell({ webhook_url: "https://hooks.example/x" })}</>);
    expect(screen.getByText(/webhook_url=/)).toBeInTheDocument();
  });

  it("summarizes a jira action via its non-secret identifiers", () => {
    render(
      <>
        {actionCell({ jira_url: "https://jira.example", project_key: "OPS", api_token: "secret" })}
      </>,
    );
    expect(screen.getByText(/jira_url=|project_key=/)).toBeInTheDocument();
  });

  it("never surfaces secret-like fields (token/password/api_key)", () => {
    const { container } = render(
      <>{actionCell({ api_token: "SUPERSECRET", password: "pw", api_key: "AKIA" })}</>,
    );
    expect(container.textContent).not.toMatch(/SUPERSECRET|AKIA/);
    expect(container.textContent).not.toMatch(/\bpw\b/);
  });
});

function notifCell(id: string, row: Partial<Notification>) {
  const col = notificationColumns.find((c) => c.id === id)!;
  return col.cell({ name: "n", ...row } as Notification);
}

describe("notification delivery-counter columns", () => {
  // The column ids ARE the server field names: serverSort forwards `sortBy`
  // straight to the CRUD `orderby`, so a rename here silently breaks sorting.
  it("sorts on the server field names", () => {
    for (const id of ["hits", "last_sent"]) {
      const col = notificationColumns.find((c) => c.id === id)!;
      expect(col.sortable).toBe(true);
    }
  });

  // Desktop invariants (W18): widths/alignment/tiers must not move as a
  // side effect of adding `cardRole`.
  it("keeps the desktop width, alignment and hideBelow tier for hits", () => {
    const col = notificationColumns.find((c) => c.id === "hits")!;
    expect(col.width).toBe("80px");
    expect(col.align).toBe("right");
    expect(col.hideBelow).toBe("md");
  });

  it("keeps the desktop width and hideBelow tier for last_sent", () => {
    const col = notificationColumns.find((c) => c.id === "last_sent")!;
    expect(col.width).toBe("110px");
    expect(col.hideBelow).toBe("lg");
  });

  // Card layout (W18): a compact, unlabelled chip that collapses entirely
  // (DataTable's `td[data-card="header"]:has(> .cellInner:empty)` rule) when
  // there's nothing to show, instead of the old "Sent — —" / "Last sent — —"
  // noise on a notification that never sent.
  it("declares cardRole 'header' on both counters so DataTable can omit them when empty", () => {
    for (const id of ["hits", "last_sent"]) {
      const col = notificationColumns.find((c) => c.id === id)!;
      expect(col.cardRole).toBe("header");
    }
  });

  it("renders hits as nothing (not an em-dash) for a notification that never sent", () => {
    const { container } = render(<>{notifCell("hits", { hits: 0 })}</>);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders hits as nothing for undefined hits", () => {
    const { container } = render(<>{notifCell("hits", {})}</>);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows the count once deliveries have happened", () => {
    render(<>{notifCell("hits", { hits: 12 })}</>);
    expect(screen.getByText("12")).toBeInTheDocument();
  });

  it("renders last_sent as nothing for a notification that never sent", () => {
    const { container } = render(<>{notifCell("last_sent", {})}</>);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders last_sent as a semantic <time> once it has a value", () => {
    const { container } = render(
      <TooltipProvider>{notifCell("last_sent", { last_sent: 1757340000 })}</TooltipProvider>,
    );
    expect(container.querySelector("time")).toHaveAttribute("datetime");
  });
});
