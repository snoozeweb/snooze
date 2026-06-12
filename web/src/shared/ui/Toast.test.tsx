import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { act } from "react";
import { Toaster, ToastProvider } from "./Toast";
import { toast, toastStore } from "./toast/useToast";

function setup() {
  return render(
    <ToastProvider>
      <Toaster />
    </ToastProvider>,
  );
}

describe("Toaster", () => {
  afterEach(() => {
    toastStore.clear();
  });

  it("renders no toasts initially", () => {
    setup();
    expect(screen.queryByText(/saved/i)).toBeNull();
  });

  it("shows a toast after toast.success() is called", async () => {
    setup();
    act(() => {
      toast.success("Saved");
    });
    expect(await screen.findByText("Saved")).toBeInTheDocument();
  });

  it("removes the toast after dismiss()", async () => {
    setup();
    let id = "";
    act(() => {
      id = toast.error("Boom");
    });
    expect(await screen.findByText("Boom")).toBeInTheDocument();
    act(() => {
      toast.dismiss(id);
    });
    await waitFor(() => expect(screen.queryByText("Boom")).toBeNull());
  });

  it("renders an action button and fires + dismisses on click (toast.undo)", async () => {
    const onUndo = vi.fn();
    const user = userEvent.setup();
    setup();
    act(() => {
      toast.undo("Acknowledged alpha", onUndo);
    });
    expect(await screen.findByText("Acknowledged alpha")).toBeInTheDocument();
    const action = screen.getByRole("button", { name: /undo/i });
    await user.click(action);
    expect(onUndo).toHaveBeenCalledTimes(1);
    // The action dismisses its own toast.
    await waitFor(() => expect(screen.queryByText("Acknowledged alpha")).toBeNull());
  });
});
