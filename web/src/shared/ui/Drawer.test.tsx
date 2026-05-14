import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { Drawer, DrawerContent, DrawerTitle, DrawerTrigger } from "./Drawer";
import { Button } from "./Button";

describe("Drawer", () => {
  it("opens on trigger and closes on Escape", async () => {
    const user = userEvent.setup();
    render(
      <Drawer>
        <DrawerTrigger>
          <Button>Edit</Button>
        </DrawerTrigger>
        <DrawerContent>
          <DrawerTitle>Edit rule</DrawerTitle>
          <p>Body content</p>
        </DrawerContent>
      </Drawer>,
    );
    await user.click(screen.getByRole("button", { name: "Edit" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("Edit rule");
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
