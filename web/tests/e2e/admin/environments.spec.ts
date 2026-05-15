// web/tests/e2e/admin/environments.spec.ts
import { test, expect } from "../harness/fixtures";

test.describe("admin / environments", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.environments.clear();
    await adminAuth();
  });

  test("create an environment", async ({ page, api, server }) => {
    await page.goto(server.baseURL + "/web/admin/environments");
    await page.getByRole("button", { name: /^new$/i }).click({ force: true });
    await expect(page.getByRole("heading", { name: /new environment/i })).toBeVisible();

    await page.locator("#environment-name").fill("e2e-prod");

    await page.getByRole("button", { name: /^create$/i }).click({ force: true });
    await expect(page.getByText(/environment created/i).first()).toBeVisible();

    const items = await api.environments.list();
    expect(items).toHaveLength(1);
  });
});
