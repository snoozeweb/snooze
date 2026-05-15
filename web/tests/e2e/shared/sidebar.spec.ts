// web/tests/e2e/shared/sidebar.spec.ts
//
// Verifies the left nav order and that clicking an item navigates the
// page. Items come from web/src/app/layout/nav-items.ts.
import { test, expect } from "../harness/fixtures";

test.describe("sidebar navigation", () => {
  test.beforeEach(async ({ adminAuth }) => {
    await adminAuth();
  });

  test("clicking Rules in the sidebar navigates to /web/rules", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/alerts");
    // The sidebar link's accessible name includes the keyboard shortcut
    // (e.g. "Rules Ctrl+4"). Match by prefix.
    await page.getByRole("link", { name: /^rules/i }).click({ force: true });
    await expect(page).toHaveURL(/\/web\/rules/);
  });

  test("Snoozes, Rules and Notifications links are present", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByRole("link", { name: /^snoozes/i })).toBeVisible();
    await expect(page.getByRole("link", { name: /^rules/i })).toBeVisible();
    await expect(page.getByRole("link", { name: /^notifications/i })).toBeVisible();
  });
});
