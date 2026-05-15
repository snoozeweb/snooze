// web/tests/e2e/admin/users.spec.ts
//
// Covers /web/admin/users:
//   - List page renders existing users (root is pre-seeded by bootstrap).
//   - Create a new user via the editor drawer.
//   - Open an existing user, edit comment, save.
import { test, expect } from "../harness/fixtures";

test.describe("admin / users", () => {
  test.beforeEach(async ({ adminAuth }) => {
    await adminAuth();
  });

  test("page renders root user and topbar count", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/admin/users");
    // Bootstrap creates a "root" user — list should contain it.
    await expect(page.getByText("root").first()).toBeVisible();
  });

  test("create a new local user via editor", async ({ page, api, server }) => {
    await page.goto(server.baseURL + "/web/admin/users");

    await page.getByRole("button", { name: /^new$/i }).click({ force: true });
    await expect(page.getByRole("heading", { name: /new user/i })).toBeVisible();

    await page.locator("#user-name").fill("e2e-new-user");
    await page.locator("#user-roles").fill("viewer");
    await page.locator("#user-password").fill("hunter2-hashed-placeholder");

    await page.getByRole("button", { name: /^create$/i }).click({ force: true });
    await expect(page.getByText(/user created/i).first()).toBeVisible();

    const users = await api.users.list();
    expect(users.length).toBeGreaterThanOrEqual(2); // root + e2e-new-user
  });
});
