import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { App } from "./App";

describe("App", () => {
  it("renders the Snooze title", () => {
    render(<App />);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Snooze");
  });

  it("renders the placeholder message", () => {
    render(<App />);
    expect(screen.getByText(/React scaffold is up/i)).toBeInTheDocument();
  });
});
