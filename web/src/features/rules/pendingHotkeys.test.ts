import { afterEach, describe, expect, it } from "vitest";
import { shouldFirePendingHotkey } from "./pendingHotkeys";

describe("shouldFirePendingHotkey", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("fires when the target isn't a field and no modal is open", () => {
    expect(shouldFirePendingHotkey(document.body)).toBe(true);
    expect(shouldFirePendingHotkey(null)).toBe(true);
  });

  it("does not fire when typing in a form field", () => {
    for (const tag of ["input", "textarea", "select"]) {
      expect(shouldFirePendingHotkey(document.createElement(tag))).toBe(false);
    }
  });

  it("does not fire while a modal dialog is open (its own Esc/Enter wins)", () => {
    // The rule editor drawer / confirm dialogs render role=dialog. Pending-mode
    // must not steal Escape and silently cancel a staged reorder underneath them.
    const dialog = document.createElement("div");
    dialog.setAttribute("role", "dialog");
    document.body.appendChild(dialog);
    expect(shouldFirePendingHotkey(document.body)).toBe(false);
  });
});
