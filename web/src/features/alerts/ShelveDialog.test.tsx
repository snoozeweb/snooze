import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ShelveDialog } from "./ShelveDialog";

const NOOP_RECORDS = [
  { uid: "r1", host: "srv-1", date_epoch: 1 } as Parameters<typeof ShelveDialog>[0]["records"][0],
];

function setup(props: Partial<Parameters<typeof ShelveDialog>[0]> = {}) {
  const onConfirm = vi.fn().mockResolvedValue(undefined);
  const onOpenChange = vi.fn();
  render(
    <ShelveDialog
      open={true}
      onOpenChange={onOpenChange}
      records={NOOP_RECORDS}
      onConfirm={onConfirm}
      {...props}
    />,
  );
  return { onConfirm, onOpenChange };
}

describe("ShelveDialog", () => {
  it("renders with 'Shelve alert' title for a single record", () => {
    setup();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText("Shelve alert")).toBeInTheDocument();
  });

  it("renders plural title for multiple records", () => {
    setup({
      records: [
        { uid: "r1", host: "srv-1", date_epoch: 1 } as Parameters<
          typeof ShelveDialog
        >[0]["records"][0],
        { uid: "r2", host: "srv-2", date_epoch: 2 } as Parameters<
          typeof ShelveDialog
        >[0]["records"][0],
      ],
    });
    expect(screen.getByText("Shelve 2 alerts")).toBeInTheDocument();
  });

  it("submits with default 4h duration when Shelve is clicked", async () => {
    const user = userEvent.setup();
    const { onConfirm } = setup();
    await user.click(screen.getByRole("button", { name: /^shelve$/i }));
    expect(onConfirm).toHaveBeenCalledWith({ duration: 14400, message: "" });
  });

  it("Cancel button calls onOpenChange(false)", async () => {
    const user = userEvent.setup();
    const { onOpenChange } = setup();
    await user.click(screen.getByRole("button", { name: /cancel/i }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});
