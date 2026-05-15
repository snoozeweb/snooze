// web/tests/e2e/dashboard/charts.spec.ts
//
// Smoke test that the dashboard renders its main cards. We don't try to
// pixel-match the charts — just verify the section headings render.
import { test, expect } from "../harness/fixtures";

test.describe("dashboard charts", () => {
  test.beforeEach(async ({ adminAuth }) => {
    await adminAuth();
  });

  test("Dashboard heading + all card titles render", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/dashboard");
    await expect(page.getByRole("heading", { name: /^dashboard$/i })).toBeVisible();
    await expect(page.getByRole("heading", { name: /records over time/i })).toBeVisible();
    await expect(page.getByRole("heading", { name: /by severity/i })).toBeVisible();
    await expect(page.getByRole("heading", { name: /by environment/i })).toBeVisible();
    await expect(page.getByRole("heading", { name: /^actions$/i })).toBeVisible();
  });
});
