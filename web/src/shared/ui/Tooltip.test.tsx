import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { Tooltip, TooltipProvider } from "./Tooltip";

function renderInProvider(ui: ReactNode) {
  return render(<TooltipProvider delay={0}>{ui}</TooltipProvider>);
}

describe("Tooltip", () => {
  it("shows the content after hovering the trigger", async () => {
    const user = userEvent.setup();
    renderInProvider(
      <Tooltip content="Refresh the list">
        <button type="button">refresh</button>
      </Tooltip>,
    );
    expect(screen.queryByRole("tooltip")).toBeNull();
    await user.hover(screen.getByRole("button"));
    expect(await screen.findByRole("tooltip")).toHaveTextContent("Refresh the list");
  });

  it("does not render when content is empty", async () => {
    const user = userEvent.setup();
    renderInProvider(
      <Tooltip content="">
        <button type="button">x</button>
      </Tooltip>,
    );
    await user.hover(screen.getByRole("button"));
    expect(screen.queryByRole("tooltip")).toBeNull();
  });
});
