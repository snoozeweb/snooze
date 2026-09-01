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

    // Filter to /web/rules by typing "rules".
    await page.getByPlaceholder(/jump to/i).fill("rules");
    await page.keyboard.press("Enter");

    await expect(page).toHaveURL(/\/web\/rules/);
  });

  test("the palette searches alerts and Enter opens the matching row", async ({
    page,
    api,
    server,
  }) => {
    await api.alerts.clear();
    await api.alerts.send({
      host: "palette-host",
      message: "cache eviction storm",
      severity: "critical",
      source: "t",
    });
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("palette-host")).toBeVisible();
    await page.locator("body").click({ force: true });
    await page.keyboard.press("Control+K");

    const dialog = page.getByRole("dialog", { name: /command palette/i });
    await expect(dialog).toBeVisible();
    await page.getByPlaceholder(/jump to/i).fill("eviction");

    // The alert surfaces under its own group, message first.
    const hit = dialog.getByRole("option", { name: /cache eviction storm/i });
    await expect(hit).toBeVisible();
    await expect(hit).toContainText("palette-host");

    // Arrow down onto it (nav entries stay first) and open it: the alerts page
    // lands filtered to that record with its detail drawer open.
    await page.keyboard.press("ArrowDown");
    await expect(hit).toHaveAttribute("aria-selected", "true");
    await page.keyboard.press("Enter");

    await expect(page).toHaveURL(/record=/);
    await expect(page.getByRole("dialog")).toBeVisible();
    await expect(page.getByRole("dialog")).toContainText("palette-host");
  });

  test("a page's context actions appear under 'On this page'", async ({
    page,
    api,
    server,
  }) => {
    await api.alerts.clear();
    await api.alerts.send({ host: "ctx-host", message: "ctx", severity: "info", source: "t" });
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("ctx-host")).toBeVisible();

    // No selection → the palette is a pure jump list.
    await page.locator("body").click({ force: true });
    await page.keyboard.press("Control+K");
    await expect(page.getByText(/on this page/i)).toHaveCount(0);
    await page.keyboard.press("Escape");

    // Select a row; the bulk verbs become reachable from the palette too.
    await page.getByRole("checkbox", { name: /select all/i }).check({ force: true });
    await expect(page.getByText("1 selected")).toBeVisible();
    await page.keyboard.press("Control+K");
    const dialog = page.getByRole("dialog", { name: /command palette/i });
    await expect(dialog.getByText(/on this page/i)).toBeVisible();
    await expect(
      dialog.getByRole("option", { name: /acknowledge selected alerts/i }),
    ).toBeVisible();
  });
});
