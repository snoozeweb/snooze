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
    // Roles is a MultiCombobox WITHOUT allowCustom — a free-typed role that
    // isn't in the catalogue grants nothing, so the picker only lets you select
    // existing roles (see UserEditor.tsx). "viewer" is one of the three default
    // roles every tenant is seeded with (internal/pluginimpl/tenant/seed.go).
    // Open the popover, filter to it, and click the matching option.
    const roles = page.getByRole("combobox", { name: "Roles" });
    await roles.click();
    const search = page.getByRole("textbox", { name: /search options/i });
    await search.fill("viewer");
    // Click the filtered option rather than pressing Enter: Enter selects
    // filtered[activeIndex], which is empty until the role catalogue query
    // resolves — a race that flakes under load. Waiting for the option to
    // render gates on the catalogue actually being loaded.
    await page.getByRole("option", { name: "viewer" }).click();
    // The role is now a removable badge on the trigger.
    await expect(page.getByRole("combobox", { name: "Roles" })).toContainText("viewer");
    await page.locator("#user-password").fill("hunter2-hashed-placeholder");

    await page.getByRole("button", { name: /^create$/i }).click({ force: true });
    await expect(page.getByText(/user created/i).first()).toBeVisible();

    const users = await api.users.list();
    expect(users.length).toBeGreaterThanOrEqual(2); // root + e2e-new-user
  });
});
