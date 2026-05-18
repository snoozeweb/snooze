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
    await expect(page.getByRole("heading", { name: /new action/i })).toBeVisible();

    await page.getByLabel("Name").fill("e2e-webhook-action");
    // #action-type is a native <select>; ActionEditor only lists plugins that
    // expose an action_form (webhook, script, mail, patlite). Pick webhook —
    // its required `url` field is a plain String input, easiest to drive.
    await page.locator("#action-type").selectOption("webhook");

    // Plugin form ids follow `action-<plugin>-<field>` (see ActionEditor
    // idPrefix={`action-${selectedPlugin.plugin_name}`} → MetadataForm fid).
    await page.locator("#action-webhook-url").fill("https://example.invalid/hook");

    await page.getByRole("button", { name: /^create$/i }).click({ force: true });
    await expect(page.getByText(/action created/i).first()).toBeVisible();

    const list = await api.actions.list();
    expect(list).toHaveLength(1);
  });
});
