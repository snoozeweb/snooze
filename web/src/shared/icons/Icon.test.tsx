import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Icon } from "./Icon";

describe("Icon", () => {
  it("renders an svg with use reference to the named symbol", () => {
    const { container } = render(<Icon name="bell" />);
    const svg = container.querySelector("svg");
    expect(svg).not.toBeNull();
    const use = svg!.querySelector("use");
    expect(use?.getAttribute("href")).toBe("/web/icons.svg#icon-bell");
  });

  it("defaults to size 16 and lets caller override", () => {
    const { container, rerender } = render(<Icon name="search" />);
    const svg = container.querySelector("svg")!;
    expect(svg.getAttribute("width")).toBe("16");
    rerender(<Icon name="search" size={20} />);
    expect(container.querySelector("svg")!.getAttribute("width")).toBe("20");
  });

  it("is hidden from assistive tech by default", () => {
    const { container } = render(<Icon name="bell" />);
    expect(container.querySelector("svg")!.getAttribute("aria-hidden")).toBe("true");
  });

  it("becomes labelled when `label` is provided", () => {
    const { container } = render(<Icon name="bell" label="Notifications" />);
    const svg = container.querySelector("svg")!;
    expect(svg.getAttribute("aria-hidden")).toBeNull();
    expect(svg.getAttribute("aria-label")).toBe("Notifications");
    expect(svg.getAttribute("role")).toBe("img");
  });

  it("forwards className to the svg root", () => {
    const { container } = render(<Icon name="bell" className="my-icon" />);
    expect(container.querySelector("svg")!.classList.contains("my-icon")).toBe(true);
  });
});
