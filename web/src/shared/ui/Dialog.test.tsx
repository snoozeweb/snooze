import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  DialogTrigger,
} from "./Dialog";
import { Button } from "./Button";

describe("Dialog", () => {
  it("opens on trigger and closes on Escape", async () => {
    const user = userEvent.setup();
    render(
      <Dialog>
        <DialogTrigger>
          <Button>Open</Button>
        </DialogTrigger>
        <DialogContent>
          <DialogTitle>Confirm delete</DialogTitle>
          <DialogDescription>This cannot be undone.</DialogDescription>
          <DialogFooter>
            <Button>Cancel</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>,
    );
    await user.click(screen.getByRole("button", { name: "Open" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("Confirm delete");
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("renders title as accessible name", async () => {
    const user = userEvent.setup();
    render(
      <Dialog>
        <DialogTrigger>
          <Button>Open</Button>
        </DialogTrigger>
        <DialogContent>
          <DialogTitle>Hello</DialogTitle>
        </DialogContent>
      </Dialog>,
    );
    await user.click(screen.getByRole("button", { name: "Open" }));
    expect(screen.getByRole("dialog", { name: "Hello" })).toBeInTheDocument();
  });
});
