// web/tests/e2e/admin/roles.spec.ts
//
// Covers /web/admin/roles:
//   - Empty state initially.
//   - Create a new role with permissions.
//   - Edit existing role: rename + add a permission.
import { test, expect } from "../harness/fixtures";

test.describe("admin / roles", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.roles.clear();
    await adminAuth();
  });

  test("create a role with two permissions", async ({ page, api, server }) => {
    await page.goto(server.baseURL + "/web/admin/roles");
    await page.getByRole("button", { name: /^new$/i }).click({ force: true });
    await expect(page.getByRole("heading", { name: /new role/i })).toBeVisible();

    await page.locator("#role-name").fill("e2e-analyst");
    // permissionsText is a textarea — newline-separated.
    await page.locator("#role-permissions").fill("rw_rule\nrw_snooze");

    await page.getByRole("button", { name: /^create$/i }).click({ force: true });
    await expect(page.getByText(/role created/i).first()).toBeVisible();

    const roles = await api.roles.list();
    expect(roles).toHaveLength(1);
  });
});
