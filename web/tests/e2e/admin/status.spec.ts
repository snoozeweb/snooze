// web/tests/e2e/admin/status.spec.ts
import { test, expect } from "../harness/fixtures";

test.describe("admin / status", () => {
  test.beforeEach(async ({ adminAuth }) => {
    await adminAuth();
  });

  test("status page renders the Status heading", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/admin/status");
    // Top-level h1 always renders, regardless of /cluster/status availability.
    await expect(page.getByRole("heading", { name: /^status$/i })).toBeVisible();
  });
});
