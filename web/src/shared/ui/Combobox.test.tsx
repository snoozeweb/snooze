import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Combobox } from "./Combobox";

const options = [
  { value: "host", label: "host" },
  { value: "message", label: "message" },
  { value: "severity", label: "severity" },
];

describe("Combobox", () => {
  it("opens on trigger click and shows options", async () => {
    const user = userEvent.setup();
    render(<Combobox options={options} placeholder="Pick field" onValueChange={() => undefined} />);
    await user.click(screen.getByRole("combobox"));
    expect(screen.getByRole("option", { name: "host" })).toBeInTheDocument();
  });

  it("filters as the user types", async () => {
    const user = userEvent.setup();
    render(<Combobox options={options} placeholder="…" onValueChange={() => undefined} />);
    await user.click(screen.getByRole("combobox"));
    await user.type(screen.getByPlaceholderText("Search…"), "sev");
    expect(screen.queryByRole("option", { name: "host" })).toBeNull();
    expect(screen.getByRole("option", { name: "severity" })).toBeInTheDocument();
  });

  it("invokes onValueChange when an option is selected", async () => {
    const handler = vi.fn();
    const user = userEvent.setup();
    render(<Combobox options={options} placeholder="…" onValueChange={handler} />);
    await user.click(screen.getByRole("combobox"));
    await user.click(screen.getByRole("option", { name: "message" }));
    expect(handler).toHaveBeenCalledWith("message");
  });
});
