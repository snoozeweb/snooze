import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { actionColumns } from "./columns";
import type { Action } from "./types";

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
      <>{actionCell({ jira_url: "https://jira.example", project_key: "OPS", api_token: "secret" })}</>,
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
