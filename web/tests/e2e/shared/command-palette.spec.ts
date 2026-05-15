// web/tests/e2e/shared/command-palette.spec.ts
//
// Cmd+K (or Ctrl+K) opens the command palette; typing filters items;
// Enter on an active row navigates to it.
import { test, expect } from "../harness/fixtures";

test.describe("command palette", () => {
  test.beforeEach(async ({ adminAuth }) => {
    await adminAuth();
  });

  test("Ctrl+K opens the palette, typing filters, Enter navigates", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/alerts");
    // Wait for app shell to mount; the global "mod+k" listener attaches once
    // AppShell's useShortcut effect runs.
    await expect(page.getByRole("link", { name: /^rules/i })).toBeVisible();
    // Click into the page so window receives keyboard events.
    await page.locator("body").click({ force: true });
    // Headless Chromium on Linux: mod = Control.
    await page.keyboard.press("Control+K");

    const dialog = page.getByRole("dialog", { name: /command palette/i });
    await expect(dialog).toBeVisible();

    // The placeholder is "Jump to…". Filter to /web/rules by typing "rules".
    await page.getByPlaceholder("Jump to…").fill("rules");
    await page.keyboard.press("Enter");

    await expect(page).toHaveURL(/\/web\/rules/);
  });
});
