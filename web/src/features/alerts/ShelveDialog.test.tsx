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

  it("submits the custom duration in seconds", async () => {
    const user = userEvent.setup();
    const { onConfirm } = setup();
    await user.selectOptions(screen.getByRole("combobox", { name: /duration/i }), "custom");
    const hours = screen.getByRole("spinbutton", { name: /hours/i });
    await user.clear(hours);
    await user.type(hours, "12");
    await user.click(screen.getByRole("button", { name: /^shelve$/i }));
    expect(onConfirm).toHaveBeenCalledWith({ duration: 43200, message: "" });
  });

  // The dialog used to promise the alert would be "silenced". It isn't:
  // notification.Process skips only ack and close, so a shelved alert that
  // recurs still notifies. The copy must describe the parking, not a silence
  // the server never delivers — and the Snooze cross-link says where to go
  // when silence is what the operator actually wanted.
  it("describes shelving as parking the row, not silencing it", () => {
    setup();
    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveTextContent(/moves to the Shelved tab until the duration expires/i);
    expect(dialog).toHaveTextContent(/returns to Open on its own/i);
    expect(dialog).not.toHaveTextContent(/silenced/i);
    expect(dialog).toHaveTextContent(/doesn't stop notifications/i);
  });

  it("Cancel button calls onOpenChange(false)", async () => {
    const user = userEvent.setup();
    const { onOpenChange } = setup();
    await user.click(screen.getByRole("button", { name: /cancel/i }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("renders a failed attempt inline and keeps the dialog open with Shelve re-enabled", () => {
    setup({
      error: {
        summary: "Couldn't shelve srv-1 — the server ran into a problem.",
        secondary: "internal server error",
      },
    });
    // Still open, not a corner toast — same Phase 3 pattern as ActionDialog.
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    const banner = screen.getByRole("alert");
    expect(banner).toHaveTextContent("Couldn't shelve srv-1");
    expect(banner).toHaveTextContent("internal server error");
    const submitButton = screen.getByRole("button", { name: /try again/i });
    expect(submitButton).not.toBeDisabled();
  });
});
