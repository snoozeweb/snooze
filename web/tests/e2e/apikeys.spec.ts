// web/tests/e2e/apikeys.spec.ts
//
// Covers the user-scoped API keys feature (plan: docs/superpowers/plans/
// 2026-06-16-user-api-keys.md, Task 13):
//   - Self-service loop on /web/profile: mint a key (name + a permission the
//     caller holds) → the raw snz_… value is revealed exactly once in a
//     read-only CopyField → the key appears in the caller's key list →
//     revoke removes it.
//   - The admin /web/admin/apikeys page renders for a user with rw_apikey
//     (the seeded root/admin token holds rw_all, which satisfies the
//     ro_apikey/rw_apikey nav + page gate), and the "API Keys" nav item is
//     visible to that admin.
//
// Selectors mirror the shipped components, NOT the plan pseudocode:
//   - ApiKeysSection: <h2>API keys</h2>, "New API key" button, per-key
//     "Revoke" → inline "Yes"/"No" confirm (never window.confirm).
//   - CreateApiKeyForm: #apikey-name input, a "Permissions" MultiCombobox
//     (role=combobox; search input aria-label "Search options"; role=option
//     entries), a "Create key" submit, then a "New API key" CopyField input
//     and a "Done" button on the show-once screen.
//   - ApiKeysPage (admin): a DataTable whose toolbar header reads "<n> API
//     keys"; the sidebar exposes an "API Keys" link.
import { test, expect } from "./harness/fixtures";

test.describe("api keys", () => {
  test.beforeEach(async ({ adminAuth }) => {
    await adminAuth();
  });

  test("self-service: mint, reveal once, list, revoke", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/profile");

    // The API keys card is the last <h2> on the profile page.
    await expect(page.getByRole("heading", { name: "API keys" })).toBeVisible();

    // Open the inline mint form.
    await page.getByRole("button", { name: /new api key/i }).click({ force: true });

    await page.locator("#apikey-name").fill("e2e-key");

    // Permissions is a MultiCombobox; the admin holds rw_all so the catalogue
    // is offered. Open the popover, filter, click a real permission option.
    const permissions = page.getByRole("combobox", { name: "Permissions" });
    await permissions.click();
    const search = page.getByRole("textbox", { name: "Search options" });
    await search.fill("ro_record");
    await page.getByRole("option", { name: "ro_record" }).click({ force: true });
    // Close the popover so the Create button is unobstructed.
    await page.keyboard.press("Escape");

    await page.getByRole("button", { name: /create key/i }).click({ force: true });

    // Show-once: the raw key is rendered in the read-only CopyField input
    // (aria-label "New API key") and must start with the snz_ prefix.
    const revealed = page.getByRole("textbox", { name: "New API key" });
    await expect(revealed).toBeVisible();
    const value = await revealed.inputValue();
    expect(value).toMatch(/^snz_/);

    // Dismiss the reveal; the key now appears in the list.
    await page.getByRole("button", { name: /^done$/i }).click({ force: true });
    await expect(page.getByText("e2e-key")).toBeVisible();

    // Revoke behind the in-DOM confirm (Revoke → Yes).
    await page.getByRole("button", { name: /^revoke$/i }).first().click({ force: true });
    await page.getByRole("button", { name: /^yes$/i }).click({ force: true });

    await expect(page.getByText("e2e-key")).toHaveCount(0);
  });

  test("admin API Keys page renders for an admin", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/admin/apikeys");

    // The page is a DataTable whose toolbar header reads "<n> API keys".
    // No keys are required for the page to render (it shows an empty state),
    // so assert on the always-present header text.
    await expect(page.getByText(/\d+ API keys/i)).toBeVisible();

    // The admin nav exposes the "API Keys" link (rw_all satisfies the gate).
    await expect(page.getByRole("link", { name: "API Keys" })).toBeVisible();
  });
});
