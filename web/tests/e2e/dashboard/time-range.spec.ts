// web/tests/e2e/dashboard/time-range.spec.ts
//
// Drives the TimeRangePicker on /web/dashboard. The preset buttons
// (1d / 1w / 1m / 1y / Custom) toggle the active range; the 'Custom'
// option reveals the shared DateTimeRangePicker — a single trigger button
// that opens a calendar + two time spinners in a popover.
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

  test("custom preset reveals the datetime range picker, which opens a calendar", async ({
    page,
    server,
  }) => {
    await page.goto(server.baseURL + "/web/dashboard");
    await page.getByRole("button", { name: /^custom$/i }).click({ force: true });

    // The shared DateTimeRangePicker renders one trigger button whose
    // accessible name embeds the From / Until labels.
    const trigger = page.getByRole("button", { name: /From \/ Until/ });
    await expect(trigger).toBeVisible();

    // Opening it reveals two <input type="time"> spinners in the popover.
    await trigger.click({ force: true });
    await expect(page.locator('input[type="time"]')).toHaveCount(2);
  });
});
