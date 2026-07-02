import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { EmptyState } from "./EmptyState";

describe("EmptyState", () => {
  it("renders the title", () => {
    render(<EmptyState title="No alerts" />);
    expect(screen.getByRole("heading", { level: 3 })).toHaveTextContent("No alerts");
  });

  it("renders the description when given", () => {
    render(<EmptyState title="No alerts" description="Nothing snoozed" />);
    expect(screen.getByText("Nothing snoozed")).toBeInTheDocument();
  });

  it("renders an icon glyph when given", () => {
    const { container } = render(<EmptyState title="x" icon="bell-off" />);
    expect(container.querySelector('use[href$="#icon-bell-off"]')).not.toBeNull();
  });

  it("renders an action slot when given", () => {
    render(<EmptyState title="x" action={<button>Refresh</button>} />);
    expect(screen.getByRole("button", { name: "Refresh" })).toBeInTheDocument();
  });

  it("renders a docs 'Learn more' link when docsSlug is given", () => {
    render(<EmptyState title="No key-values yet" docsSlug="general/kv" />);
    const link = screen.getByRole("link");
    expect(link).toHaveAttribute("href", "https://snoozeweb.github.io/snooze/general/kv");
    expect(link).toHaveAttribute("target", "_blank");
  });

  it("renders no docs link when docsSlug is omitted", () => {
    render(<EmptyState title="x" description="y" />);
    expect(screen.queryByRole("link")).toBeNull();
  });
});
