// web/tests/e2e/admin/roles.spec.ts
//
// Covers /web/admin/roles:
//   - Empty state initially.
//   - Create a new role with permissions.
//   - Edit existing role: rename + add a permission.
import { test, expect } from "../harness/fixtures";

test.describe("admin / roles", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    // clear() must not error on the seeded platform_admin role: the server's role
    // plugin GuardDelete returns 403 for reserved roles. We override clear() for
    // roles only — the generic resourceApi.remove() throws on any non-404 error,
    // so we re-implement cleanup here and swallow 403 (protected/built-in roles).
    const roles = await api.roles.list();
    for (const r of roles) {
      try {
        await api.roles.remove(r.uid);
      } catch (e: unknown) {
        // 403 = reserved role (platform_admin); skip it and continue cleanup.
        if (e instanceof Error && e.message.includes("403")) continue;
        throw e;
      }
    }
    await adminAuth();
  });

  test("create a role with two permissions", async ({ page, api, server }) => {
    await page.goto(server.baseURL + "/web/admin/roles");
    await page.getByRole("button", { name: /^new$/i }).click({ force: true });
    await expect(page.getByRole("heading", { name: /new role/i })).toBeVisible();

    await page.locator("#role-name").fill("e2e-analyst");
    // Permissions is a MultiCombobox bound to /api/v1/permissions (no
    // allowCustom — picks must exist in the catalogue). Open the popover,
    // filter, then click the matching option for each permission.
    const permissions = page.getByRole("combobox", { name: "Permissions" });
    await permissions.click();
    const search = page.getByPlaceholder("Search");
    await search.fill("rw_rule");
    await page.getByRole("option", { name: "rw_rule" }).click({ force: true });
    await search.fill("rw_snooze");
    await page.getByRole("option", { name: "rw_snooze" }).click({ force: true });

    await page.getByRole("button", { name: /^create$/i }).click({ force: true });
    await expect(page.getByText(/role created/i).first()).toBeVisible();

    // platform_admin is a seeded protected role that survives clear(); filter it out
    // so the assertion only counts roles created by this test.
    const allRoles = await api.roles.list();
    const userRoles = allRoles.filter((r) => (r as Record<string, unknown>)["name"] !== "platform_admin");
    expect(userRoles).toHaveLength(1);
  });
});
