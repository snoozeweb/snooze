import { readFileSync } from "node:fs";
import { join } from "node:path";
import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { BrandIcon } from "./BrandIcon";
import { BRAND_NAMES, brandFor } from "./brand-names";

describe("BrandIcon", () => {
  it("renders an svg with a use reference to the named brand symbol", () => {
    const { container } = render(<BrandIcon name="slack" />);
    const use = container.querySelector("svg use");
    expect(use?.getAttribute("href")).toBe("/web/brands.svg#brand-slack");
  });

  it("fills with currentColor so it inherits the surrounding text color", () => {
    const { container } = render(<BrandIcon name="slack" />);
    const svg = container.querySelector("svg")!;
    expect(svg.getAttribute("fill")).toBe("currentColor");
    // Brand marks are fill-based, not stroke-based like the Lucide Icon.
    expect(svg.getAttribute("stroke")).toBeNull();
  });

  it("defaults to size 16 and lets the caller override", () => {
    const { container, rerender } = render(<BrandIcon name="slack" />);
    expect(container.querySelector("svg")!.getAttribute("width")).toBe("16");
    rerender(<BrandIcon name="slack" size={24} />);
    expect(container.querySelector("svg")!.getAttribute("width")).toBe("24");
  });

  it("is hidden from assistive tech by default and labelled on request", () => {
    const { container, rerender } = render(<BrandIcon name="slack" />);
    expect(container.querySelector("svg")!.getAttribute("aria-hidden")).toBe("true");
    rerender(<BrandIcon name="slack" label="Slack" />);
    const svg = container.querySelector("svg")!;
    expect(svg.getAttribute("aria-hidden")).toBeNull();
    expect(svg.getAttribute("aria-label")).toBe("Slack");
    expect(svg.getAttribute("role")).toBe("img");
  });

  it("forwards className to the svg root", () => {
    const { container } = render(<BrandIcon name="slack" className="my-brand" />);
    expect(container.querySelector("svg")!.classList.contains("my-brand")).toBe(true);
  });
});

describe("brandFor", () => {
  it("maps a known notifier plugin_name to its brand glyph", () => {
    expect(brandFor("slack")).toBe("slack");
    expect(brandFor("teams")).toBe("teams");
    expect(brandFor("googlechat")).toBe("googlechat");
    expect(brandFor("sns")).toBe("sns");
  });

  it("returns null for plugins without a vendored brand glyph", () => {
    expect(brandFor("mail")).toBeNull();
    expect(brandFor("webhook")).toBeNull();
    expect(brandFor("script")).toBeNull();
    // No brand mark exists in Simple Icons for these.
    expect(brandFor("servicenow")).toBeNull();
    expect(brandFor("pushover")).toBeNull();
    expect(brandFor(undefined)).toBeNull();
    expect(brandFor(null)).toBeNull();
  });

  it("matches the brand ids vendored in the sprite", () => {
    expect(BRAND_NAMES).toContain("slack");
    expect(BRAND_NAMES).toContain("sns");
    expect(BRAND_NAMES).not.toContain("mail");
  });
});

describe("brands.svg sprite", () => {
  // Lockstep guard: every brand the registry advertises must actually exist as
  // a <symbol id="brand-…"> in the shipped sprite, or BrandIcon renders blank.
  // Vitest runs with the web/ project as cwd (vitest.config.ts root).
  const sprite = readFileSync(join(process.cwd(), "public/brands.svg"), "utf8");

  it.each(BRAND_NAMES)("has a symbol for %s", (name) => {
    expect(sprite).toContain(`id="brand-${name}"`);
  });
});
