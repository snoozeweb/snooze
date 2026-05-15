import { render, screen, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { MultiCombobox } from "./MultiCombobox";

const OPTIONS = [
  { value: "admin", label: "admin" },
  { value: "viewer", label: "viewer" },
  { value: "analyst", label: "analyst" },
];

describe("MultiCombobox", () => {
  it("renders selected values as removable badges", () => {
    render(
      <MultiCombobox
        options={OPTIONS}
        value={["admin", "viewer"]}
        onChange={() => undefined}
        aria-label="Roles"
      />,
    );
    expect(screen.getByText("admin")).toBeInTheDocument();
    expect(screen.getByText("viewer")).toBeInTheDocument();
    expect(screen.getByLabelText("Remove admin")).toBeInTheDocument();
  });

  it("removes a value when the × button is clicked", () => {
    const onChange = vi.fn();
    render(
      <MultiCombobox
        options={OPTIONS}
        value={["admin", "viewer"]}
        onChange={onChange}
        aria-label="Roles"
      />,
    );
    fireEvent.click(screen.getByLabelText("Remove admin"));
    expect(onChange).toHaveBeenCalledWith(["viewer"]);
  });

  it("appends a new value via the search popover", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <MultiCombobox
        options={OPTIONS}
        value={[]}
        onChange={onChange}
        aria-label="Roles"
      />,
    );
    // Open the popover.
    await user.click(screen.getByRole("combobox", { name: "Roles" }));
    // Click the "viewer" option in the popover.
    await user.click(screen.getByRole("option", { name: /viewer/ }));
    expect(onChange).toHaveBeenCalledWith(["viewer"]);
  });

  it("when allowCustom, Enter on a missing value adds it as a tag", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <MultiCombobox
        options={OPTIONS}
        value={[]}
        onChange={onChange}
        aria-label="Roles"
        allowCustom
      />,
    );
    await user.click(screen.getByRole("combobox", { name: "Roles" }));
    const search = screen.getByPlaceholderText(/search or type/i);
    await user.type(search, "custom-role{Enter}");
    expect(onChange).toHaveBeenCalledWith(["custom-role"]);
  });
});
