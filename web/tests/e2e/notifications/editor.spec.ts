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

  test("create a script action", async ({ page, api, server }) => {
    await page.goto(server.baseURL + "/web/notifications");
    // Switch to the Actions tab so the "New" button opens ActionEditor.
    await page.getByRole("tab", { name: /^actions$/i }).click({ force: true });
    await page.getByRole("button", { name: /^new$/i }).click({ force: true });
    await expect(page.getByRole("heading", { name: /new action/i })).toBeVisible();

    await page.getByLabel("Name").fill("e2e-script-action");
    // The Type input has label "Type" (id="action-type"). Leave default
    // "script" — but assert by filling explicitly.
    await page.getByLabel("Type").fill("script");

    // Config (JSON) textarea inside the "Config (JSON)" section.
    // Replace its content with a minimal object.
    const cfg = page.locator('textarea[name="action_json"]');
    await cfg.fill('{"command":"/bin/true"}');

    await page.getByRole("button", { name: /^create$/i }).click({ force: true });
    await expect(page.getByText(/action created/i).first()).toBeVisible();

    const list = await api.actions.list();
    expect(list).toHaveLength(1);
  });
});
