import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import { makeInputColumns } from "./columns";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import type { InputRow } from "./types";

function cell(id: string, row: InputRow, onSetup = vi.fn()) {
  const col = makeInputColumns(onSetup).find((c) => c.id === id)!;
  return { node: col.cell(row), onSetup };
}

describe("makeInputColumns", () => {
  it("renders 'never' for an idle catalogue input", () => {
    render(
      <>{cell("last", { id: "syslog", name: "Syslog", family: "daemon", catalogue: true }).node}</>,
    );
    expect(screen.getByText("never")).toBeInTheDocument();
  });

  it("renders '—' for REST", () => {
    render(
      <>
        {
          cell("last", {
            id: "rest",
            name: "REST API",
            family: "rest",
            catalogue: true,
            restNoActivity: true,
          }).node
        }
      </>,
    );
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("renders a TimeCell when lastEpoch is set", () => {
    render(
      <TooltipProvider>
        {
          cell("last", {
            id: "grafana",
            name: "Grafana",
            family: "webhook",
            catalogue: true,
            lastEpoch: 1_700_000_000,
          }).node
        }
      </TooltipProvider>,
    );
    // TimeCell renders a semantic <time> element with the epoch as its ISO datetime.
    expect(document.querySelector("time")).toBeInTheDocument();
  });

  it("renders a docs link when docSlug is set", () => {
    render(
      <>
        {
          cell("docs", {
            id: "grafana",
            name: "Grafana",
            family: "webhook",
            catalogue: true,
            docSlug: "general/integrations/grafana",
          }).node
        }
      </>,
    );
    expect(screen.getByRole("link")).toHaveAttribute(
      "href",
      "https://snoozeweb.github.io/snooze/general/integrations/grafana",
    );
  });

  it("renders '—' for docs when docSlug is absent", () => {
    render(
      <>
        {cell("docs", { id: "graylog", name: "graylog", family: "other", catalogue: false }).node}
      </>,
    );
    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.queryByRole("link")).toBeNull();
  });

  it("fires onSetup from the Setup button", async () => {
    const { node, onSetup } = cell("setup", {
      id: "grafana",
      name: "Grafana",
      family: "webhook",
      catalogue: true,
    });
    render(<>{node}</>);
    await userEvent.click(screen.getByRole("button", { name: /setup/i }));
    expect(onSetup).toHaveBeenCalledWith("grafana");
  });

  it("renders no Setup button for an 'other' row", () => {
    render(
      <>
        {cell("setup", { id: "graylog", name: "graylog", family: "other", catalogue: false }).node}
      </>,
    );
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("renders a Type badge with the family label", () => {
    render(
      <>
        {cell("family", { id: "syslog", name: "Syslog", family: "daemon", catalogue: true }).node}
      </>,
    );
    expect(screen.getByText("Daemon")).toBeInTheDocument();
  });
});
