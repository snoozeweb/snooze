// web/tests/e2e/admin/kv.spec.ts
import { test, expect } from "../harness/fixtures";

test.describe("admin / kv", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.kv.clear();
    await adminAuth();
  });

  test("create a key/value entry", async ({ page, api, server }) => {
    await page.goto(server.baseURL + "/web/admin/kv");
    await page.getByRole("button", { name: /^new$/i }).click({ force: true });
    await expect(page.getByRole("heading", { name: /new key-value/i })).toBeVisible();

    // dict + key are both required by the server (kv plugin Validate rejects empty dict).
    await page.locator("#kv-dict").fill("e2e_dict");
    await page.locator("#kv-key").fill("E2E_KEY");
    await page.locator("#kv-value").fill("a-value");

    await page.getByRole("button", { name: /^create$/i }).click({ force: true });
    await expect(page.getByText(/key-value created/i).first()).toBeVisible();

    const items = await api.kv.list();
    expect(items).toHaveLength(1);
  });
});
