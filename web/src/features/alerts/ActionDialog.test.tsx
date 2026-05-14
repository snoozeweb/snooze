import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ActionDialog } from "./ActionDialog";
import type { Record_ } from "./types";

const oneRecord: Record_[] = [{ uid: "r1", host: "srv-1" }];
const threeRecords: Record_[] = [
  { uid: "r1", host: "srv-1" },
  { uid: "r2", host: "srv-2" },
  { uid: "r3", host: "srv-3" },
];

describe("ActionDialog", () => {
  it("does not render when closed", () => {
    render(
      <ActionDialog
        open={false}
        onOpenChange={() => undefined}
        actionType="ack"
        records={oneRecord}
        onConfirm={() => Promise.resolve()}
      />,
    );
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("titles for single Acknowledge: includes the host", () => {
    render(
      <ActionDialog
        open
        onOpenChange={() => undefined}
        actionType="ack"
        records={oneRecord}
        onConfirm={() => Promise.resolve()}
      />,
    );
    expect(screen.getByRole("dialog", { name: /acknowledge/i })).toBeInTheDocument();
    expect(screen.getByText(/srv-1/)).toBeInTheDocument();
  });

  it("titles for bulk: includes the count", () => {
    render(
      <ActionDialog
        open
        onOpenChange={() => undefined}
        actionType="close"
        records={threeRecords}
        onConfirm={() => Promise.resolve()}
      />,
    );
    expect(screen.getByRole("dialog", { name: /close 3 alerts/i })).toBeInTheDocument();
  });

  it("Confirm calls onConfirm with the typed message", async () => {
    const onConfirm = vi.fn().mockResolvedValue(undefined);
    const user = userEvent.setup();
    render(
      <ActionDialog
        open
        onOpenChange={() => undefined}
        actionType="comment"
        records={oneRecord}
        onConfirm={onConfirm}
      />,
    );
    await user.type(screen.getByPlaceholderText(/comment/i), "looking now");
    await user.click(screen.getByRole("button", { name: /^comment$/i }));
    expect(onConfirm).toHaveBeenCalledWith({ message: "looking now" });
  });

  it("Comment requires a non-empty message", async () => {
    const onConfirm = vi.fn();
    const user = userEvent.setup();
    render(
      <ActionDialog
        open
        onOpenChange={() => undefined}
        actionType="comment"
        records={oneRecord}
        onConfirm={onConfirm}
      />,
    );
    await user.click(screen.getByRole("button", { name: /^comment$/i }));
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("Cancel closes the dialog without calling onConfirm", async () => {
    const onConfirm = vi.fn();
    const onOpenChange = vi.fn();
    const user = userEvent.setup();
    render(
      <ActionDialog
        open
        onOpenChange={onOpenChange}
        actionType="ack"
        records={oneRecord}
        onConfirm={onConfirm}
      />,
    );
    await user.click(screen.getByRole("button", { name: /cancel/i }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("disables Confirm while submitting=true", () => {
    render(
      <ActionDialog
        open
        onOpenChange={() => undefined}
        actionType="ack"
        records={oneRecord}
        onConfirm={() => Promise.resolve()}
        submitting
      />,
    );
    expect(screen.getByRole("button", { name: /acknowledge/i })).toBeDisabled();
  });
});
