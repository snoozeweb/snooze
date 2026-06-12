// web/tests/e2e/notifications/editor.spec.ts
//
// Exercises NotificationEditor and ActionEditor.
//   - Create a notification with a name + one comma-separated action.
//   - Create an action with type=script + JSON config.
import { test, expect } from "../harness/fixtures";

test.describe("notification editor", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.notifications.clear();
    await adminAuth();
  });

  test("create a notification with one action", async ({ page, api, server }) => {
    await page.goto(server.baseURL + "/web/notifications");
    await page.getByRole("button", { name: /^new$/i }).click({ force: true });
    await expect(page.getByRole("heading", { name: /new notification/i })).toBeVisible();

    await page.locator("#notif-name").fill("e2e-notif");
    // Actions is now a MultiCombobox; click it to open the popover, type
    // the name (allowCustom is on), and Enter to add it as a badge.
    const actions = page.getByRole("combobox", { name: "Actions" });
    await actions.click();
    const search = page.getByPlaceholder(/search or type/i);
    await search.fill("slack-prod");
    await search.press("Enter");

    await page.getByRole("button", { name: /^create$/i }).click({ force: true });
    await expect(page.getByText(/notification created/i).first()).toBeVisible();

    const list = await api.notifications.list();
    expect(list).toHaveLength(1);
  });
});

test.describe("action editor", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.actions.clear();
    await adminAuth();
  });

  test("create a webhook action", async ({ page, api, server }) => {
    await page.goto(server.baseURL + "/web/notifications");
    // Switch to the Actions tab so the "New" button opens ActionEditor.
    await page.getByRole("tab", { name: /^actions$/i }).click({ force: true });
    await page.getByRole("button", { name: /^new$/i }).click({ force: true });
    // ActionEditor opens in the "pick" step — it shows an IntegrationGallery
    // (heading: "Choose an integration") before the config form. We must click
    // through the gallery to select a provider before the form appears.
    await expect(page.getByRole("heading", { name: /choose an integration/i })).toBeVisible();
    // The gallery renders one <button> per plugin; webhook's display name is
    // "Call a webhook" (metadata.yaml name field, plugin_name = "webhook").
    await page.getByRole("button", { name: /call a webhook/i }).click({ force: true });
    // After picking, the editor advances to the config form.
    await expect(page.getByRole("heading", { name: /new call a webhook action/i })).toBeVisible();

    await page.getByLabel("Name").fill("e2e-webhook-action");
    // Plugin form ids follow `action-<plugin_name>-<field>` (see ActionEditor
    // idPrefix={`action-${selectedPlugin.plugin_name}`} → MetadataForm fid).
    await page.locator("#action-webhook-url").fill("https://example.invalid/hook");

    await page.getByRole("button", { name: /^create$/i }).click({ force: true });
    await expect(page.getByText(/action created/i).first()).toBeVisible();

    const list = await api.actions.list();
    expect(list).toHaveLength(1);
  });
});
