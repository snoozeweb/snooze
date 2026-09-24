// web/tests/e2e/alerts/deliveries.spec.ts
//
// The reverse view of the delivery history: standing on ONE alert, who was
// told about it and when. The alert inspector's 4th tab plus the "Last
// notified …" line in its header.
//
// Same timing contract as notifications/deliveries.spec.ts: the ingest POST
// returns before the dispatcher has sent anything, so the test waits on the
// delivery log via the API and only then drives the UI.
import { test, expect } from "../harness/fixtures";

test.describe("alert inspector deliveries", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.notifications.clear();
    await api.actions.clear();
    await api.alerts.clear();
    await api.notificationlog.clear();
    await adminAuth();
  });

  test("the drawer says who was notified, and the Deliveries tab lists the send", async ({
    page,
    api,
    server,
  }) => {
    await api.actions.create({
      name: "act-alert-view",
      action: { selected: "script", subcontent: { command: ["/bin/true"] } },
    });
    await api.notifications.create({
      name: "notif-alert-view",
      enabled: true,
      condition: { type: "=", field: "source", value: "e2e-alert-view" },
      actions: ["act-alert-view"],
    });
    await api.warmUpDispatcher();

    await api.alerts.send({
      host: "srv-alert-view",
      message: "paged about this",
      severity: "critical",
      source: "e2e-alert-view",
    });
    await api.waitForDeliveries(1);

    await page.goto(server.baseURL + "/web/alerts");
    await page
      .locator("tr", { hasText: "srv-alert-view" })
      .first()
      .getByRole("button", { name: "View details" })
      .click({ force: true });
    const drawer = page.getByRole("dialog");
    await expect(drawer).toBeVisible();

    // The header answers "was anyone told?" before any tab is touched: its
    // Notified fact names the action that last sent.
    const lastNotified = drawer.locator('dt:text-is("Notified") + dd');
    await expect(lastNotified).toBeVisible();
    await expect(lastNotified).toContainText("via act-alert-view");

    // The tab carries the count, so an alert with history is distinguishable
    // from one without at a glance.
    const tab = drawer.getByRole("tab", { name: "Deliveries · 1" });
    await expect(tab).toBeVisible();
    await tab.click({ force: true });

    const item = drawer.getByRole("listitem", { name: /^Sent via act-alert-view, 1 alert/ });
    await expect(item).toHaveCount(1);
    await expect(item.locator("time")).toBeVisible();
    // The chip names the action and colours the outcome; the status word and
    // the per-send time are in its tooltip and its accessible name.
    await expect(item.getByText("act-alert-view", { exact: true })).toBeVisible();
    await expect(item.getByTitle(/^act-alert-view · script — Sent,/)).toBeVisible();
    // On the alert inspector the row IS the alert, so it names the route that
    // paged rather than repeating the alert line.
    await expect(item.getByText("notif-alert-view", { exact: true })).toBeVisible();
  });
  test("one notification's actions share a row, each chip carrying its outcome", async ({
    page,
    api,
    server,
  }) => {
    // Two actions on one notification: the log writes two rows (one per
    // action), the inspector shows one dispatch with two chips.
    await api.actions.create({
      name: "act-fanout-ok",
      action: { selected: "script", subcontent: { command: ["/bin/true"] } },
    });
    await api.actions.create({
      name: "act-fanout-fail",
      action: { selected: "script", subcontent: { command: ["/bin/false"] } },
    });
    await api.notifications.create({
      name: "notif-fanout",
      enabled: true,
      condition: { type: "=", field: "source", value: "e2e-alert-fanout" },
      actions: ["act-fanout-ok", "act-fanout-fail"],
    });
    await api.warmUpDispatcher();

    await api.alerts.send({
      host: "srv-alert-fanout",
      message: "two actions, one dispatch",
      severity: "critical",
      source: "e2e-alert-fanout",
    });
    await api.waitForDeliveries(2);

    await page.goto(server.baseURL + "/web/alerts");
    await page
      .locator("tr", { hasText: "srv-alert-fanout" })
      .first()
      .getByRole("button", { name: "View details" })
      .click({ force: true });
    const drawer = page.getByRole("dialog");
    // The tab still counts SENDS: grouping changes the reading, not the log.
    await drawer.getByRole("tab", { name: "Deliveries · 2" }).click({ force: true });

    const rows = drawer.getByRole("listitem", { name: /via/ });
    await expect(rows).toHaveCount(1);
    const item = rows.first();
    await expect(item).toHaveAccessibleName(
      /^Sent via act-fanout-ok — failed via act-fanout-fail, 1 alert/,
    );
    await expect(item.getByText("act-fanout-ok", { exact: true })).toBeVisible();
    await expect(item.getByText("act-fanout-fail", { exact: true })).toBeVisible();
    // One timestamp for the dispatch, not one per action.
    await expect(item.locator("time")).toHaveCount(1);
    // The failure names which action produced it.
    await expect(item.locator("pre")).toContainText("act-fanout-fail:");
    await expect(item.locator("pre")).toContainText("script: exit 1");
  });
});
