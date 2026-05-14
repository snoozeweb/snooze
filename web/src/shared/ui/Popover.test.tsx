import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { Popover, PopoverContent, PopoverTrigger } from "./Popover";

describe("Popover", () => {
  it("opens on trigger click", async () => {
    const user = userEvent.setup();
    render(
      <Popover>
        <PopoverTrigger>open</PopoverTrigger>
        <PopoverContent>panel</PopoverContent>
      </Popover>,
    );
    expect(screen.queryByText("panel")).toBeNull();
    await user.click(screen.getByText("open"));
    expect(screen.getByText("panel")).toBeInTheDocument();
  });

  it("closes on Escape", async () => {
    const user = userEvent.setup();
    render(
      <Popover>
        <PopoverTrigger>open</PopoverTrigger>
        <PopoverContent>panel</PopoverContent>
      </Popover>,
    );
    await user.click(screen.getByText("open"));
    await user.keyboard("{Escape}");
    expect(screen.queryByText("panel")).toBeNull();
  });
});
