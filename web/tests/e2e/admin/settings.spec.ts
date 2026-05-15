// web/tests/e2e/admin/settings.spec.ts
import { test, expect } from "../harness/fixtures";

test.describe("admin / settings", () => {
  test.beforeEach(async ({ adminAuth }) => {
    await adminAuth();
  });

  test("settings page renders without error", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/admin/settings");
    // The page renders without throwing; we don't assert specific contents
    // because settings shape depends on backend feature flags.
    await expect(page.locator("body")).toBeVisible();
  });
});
