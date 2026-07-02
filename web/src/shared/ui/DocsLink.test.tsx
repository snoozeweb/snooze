import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { DocsLink } from "./DocsLink";

describe("DocsLink", () => {
  it("links to the docs page for the slug and opens safely in a new tab", () => {
    render(<DocsLink slug="general/rules" />);
    const link = screen.getByRole("link");
    expect(link).toHaveAttribute("href", "https://snoozeweb.github.io/snooze/general/rules");
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noreferrer");
    // Sensible default label so callers can drop it in without boilerplate.
    expect(link).toHaveTextContent("Learn more");
  });

  it("uses custom children as the link text", () => {
    render(<DocsLink slug="general/kv">KV docs</DocsLink>);
    expect(screen.getByRole("link")).toHaveTextContent("KV docs");
  });
});
