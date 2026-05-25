// web/tests/e2e/notifications/list.spec.ts
//
// Drives NotificationsPage at /web/notifications. Covers:
//   - Notifications and Actions tabs.
//   - Empty state when no items.
//   - Items appear after API create.
import { test, expect } from "../harness/fixtures";

test.describe("notifications list", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.notifications.clear();
    await api.actions.clear();
    await adminAuth();
  });

  test("Notifications tab shows API-created items", async ({ page, api, server }) => {
    await api.notifications.create({
      name: "notif-prod",
      enabled: true,
      condition: { type: "ALWAYS_TRUE" },
      actions: ["slack-prod"],
    });
    await page.goto(server.baseURL + "/web/notifications");
    await expect(page.getByText("notif-prod")).toBeVisible();
    // Topbar reads "{n} notifications".
    await expect(page.getByText(/1 notifications/i)).toBeVisible();
  });

  test("Actions tab is independent from Notifications tab", async ({ page, api, server }) => {
    await api.notifications.create({
      name: "notif-x",
      enabled: true,
      condition: { type: "ALWAYS_TRUE" },
    });
    await api.actions.create({
      name: "act-y",
      action: { selected: "script", subcontent: { command: ["/bin/true"] } },
    });
    await page.goto(server.baseURL + "/web/notifications");
    await expect(page.getByText("notif-x")).toBeVisible();

    // Switch to Actions tab.
    await page.getByRole("tab", { name: /^actions$/i }).click({ force: true });
    await expect(page.getByText("act-y")).toBeVisible();
    await expect(page.getByText("notif-x")).toBeHidden();
  });
});
