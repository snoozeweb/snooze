import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { RadioGroup, RadioOption } from "./Radio";

describe("Radio", () => {
  it("renders options and switches on click", async () => {
    const onValueChange = vi.fn();
    const user = userEvent.setup();
    render(
      <RadioGroup defaultValue="a" onValueChange={onValueChange}>
        <RadioOption value="a" aria-label="A" />
        <RadioOption value="b" aria-label="B" />
      </RadioGroup>,
    );
    await user.click(screen.getByRole("radio", { name: "B" }));
    expect(onValueChange).toHaveBeenCalledWith("b");
  });
});
