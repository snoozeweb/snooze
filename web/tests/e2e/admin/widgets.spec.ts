// web/tests/e2e/admin/widgets.spec.ts
import { test, expect } from "../harness/fixtures";

test.describe("admin / widgets", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.widgets.clear();
    await adminAuth();
  });

  test("create a widget with empty config", async ({ page, api, server }) => {
    await page.goto(server.baseURL + "/web/admin/widgets");
    await page.getByRole("button", { name: /^new$/i }).click({ force: true });
    await expect(page.getByRole("heading", { name: /new widget/i })).toBeVisible();

    await page.locator("#widget-name").fill("e2e-widget");
    await page.locator("#widget-type").fill("iframe");
    // Default config is {} — fine.

    await page.getByRole("button", { name: /^create$/i }).click({ force: true });
    await expect(page.getByText(/widget created/i).first()).toBeVisible();

    const items = await api.widgets.list();
    expect(items).toHaveLength(1);
  });
});
