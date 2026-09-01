import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { MessageCell } from "./MessageCell";

function renderCell(text: string) {
  render(
    <TooltipProvider>
      <MessageCell text={text} />
    </TooltipProvider>,
  );
}

describe("MessageCell", () => {
  it("renders the message text", () => {
    renderCell("disk almost full on /var/lib/postgresql");
    expect(screen.getByText("disk almost full on /var/lib/postgresql")).toBeInTheDocument();
  });

  it("adds no tooltip trigger when the clamp isn't biting", async () => {
    // jsdom reports zero for every layout metric, so scrollHeight never exceeds
    // clientHeight — the same branch a short message takes in the browser.
    // Hovering must therefore leave the span bare rather than wrapping it in a
    // Radix trigger, which is what keeps short messages tooltip-free.
    const user = userEvent.setup();
    renderCell("healthcheck ok");
    const span = screen.getByText("healthcheck ok");
    await user.hover(span);
    expect(span).not.toHaveAttribute("data-state");
  });
});
