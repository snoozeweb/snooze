import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ModificationList } from "./ModificationList";
import type { Modification } from "./types";

function Controlled({ initial }: { initial: Modification[] }) {
  const [value, setValue] = useState<Modification[]>(initial);
  return <ModificationList value={value} onChange={setValue} />;
}

describe("ModificationList", () => {
  it("renders 'Add modification' when empty", () => {
    const onChange = vi.fn();
    render(<ModificationList value={[]} onChange={onChange} />);
    expect(screen.getByRole("button", { name: /add modification/i })).toBeInTheDocument();
  });

  it("clicking Add appends a default 'set' modification", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<ModificationList value={[]} onChange={onChange} />);
    await user.click(screen.getByRole("button", { name: /add modification/i }));
    expect(onChange).toHaveBeenCalledWith([{ type: "set", field: "", value: "" }]);
  });

  it("renders one row per modification with field + value inputs", () => {
    const value: Modification[] = [
      { type: "set", field: "severity", value: "critical" },
      { type: "delete", field: "noisy_field" },
    ];
    render(<ModificationList value={value} onChange={() => undefined} />);
    expect(screen.getAllByPlaceholderText(/field/i)).toHaveLength(2);
    expect(screen.getByDisplayValue("critical")).toBeInTheDocument();
  });

  it("editing a field invokes onChange with the updated array", async () => {
    const user = userEvent.setup();
    render(<Controlled initial={[{ type: "set", field: "host", value: "x" }]} />);
    const fieldInput = screen.getByDisplayValue("host");
    await user.clear(fieldInput);
    await user.type(fieldInput, "environment");
    expect(screen.getByDisplayValue("environment")).toBeInTheDocument();
  });

  it("changing type from set to delete drops the value field", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    const value: Modification[] = [{ type: "set", field: "x", value: "1" }];
    render(<ModificationList value={value} onChange={onChange} />);
    // Radix combobox doesn't expose accessible name from text content; query by role index
    const [combobox] = screen.getAllByRole("combobox");
    await user.click(combobox!);
    await user.click(screen.getByRole("option", { name: /^delete$/i }));
    const last = onChange.mock.calls.at(-1)?.[0] as Modification[];
    expect(last[0]).toEqual({ type: "delete", field: "x" });
  });

  it("remove button removes the row", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    const value: Modification[] = [
      { type: "set", field: "a", value: "1" },
      { type: "set", field: "b", value: "2" },
    ];
    render(<ModificationList value={value} onChange={onChange} />);
    const removeButtons = screen.getAllByRole("button", { name: /^remove$/i });
    await user.click(removeButtons[0]!);
    const last = onChange.mock.calls.at(-1)?.[0] as Modification[];
    expect(last).toHaveLength(1);
    expect(last[0]?.field).toBe("b");
  });
});
