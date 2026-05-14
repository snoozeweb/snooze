import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Skeleton } from "./Skeleton";

describe("Skeleton", () => {
  it("renders with default radius=md", () => {
    render(<Skeleton />);
    expect(screen.getByTestId("skeleton").className).toMatch(/md/);
  });

  it("accepts numeric width/height as px", () => {
    render(<Skeleton width={120} height={24} />);
    const el = screen.getByTestId("skeleton") as HTMLElement;
    expect(el.style.width).toBe("120px");
    expect(el.style.height).toBe("24px");
  });

  it("accepts string width/height verbatim", () => {
    render(<Skeleton width="50%" height="2em" />);
    const el = screen.getByTestId("skeleton") as HTMLElement;
    expect(el.style.width).toBe("50%");
    expect(el.style.height).toBe("2em");
  });

  it("is aria-hidden", () => {
    render(<Skeleton />);
    expect(screen.getByTestId("skeleton")).toHaveAttribute("aria-hidden", "true");
  });
});
