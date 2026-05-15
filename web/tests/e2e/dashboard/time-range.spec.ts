// web/tests/e2e/dashboard/time-range.spec.ts
//
// Drives the TimeRangePicker on /web/dashboard. The preset buttons
// (1d / 1w / 1m / 1y / Custom) toggle the active range; the 'Custom'
// option reveals two date inputs.
import { test, expect } from "../harness/fixtures";

test.describe("dashboard time-range picker", () => {
  test.beforeEach(async ({ adminAuth }) => {
    await adminAuth();
  });

  test("preset buttons render and 1w activates on click", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/dashboard");
    await expect(page.getByRole("button", { name: /^1d$/ })).toBeVisible();

    await page.getByRole("button", { name: /^1w$/ }).click({ force: true });
    // data-active attribute marks the selected preset.
    await expect(page.getByRole("button", { name: /^1w$/ })).toHaveAttribute("data-active", "true");
  });

  test("custom preset reveals two date inputs", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/dashboard");
    await page.getByRole("button", { name: /^custom$/i }).click({ force: true });
    // Two date inputs appear.
    await expect(page.locator('input[type="date"]')).toHaveCount(2);
  });
});
