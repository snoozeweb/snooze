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

    // The header answers "was anyone told?" before any tab is touched.
    const lastNotified = drawer.getByText(/Last notified/);
    await expect(lastNotified).toBeVisible();
    await expect(lastNotified).toContainText("act-alert-view");

    // The tab carries the count, so an alert with history is distinguishable
    // from one without at a glance.
    const tab = drawer.getByRole("tab", { name: "Deliveries · 1" });
    await expect(tab).toBeVisible();
    await tab.click({ force: true });

    const item = drawer.getByRole("listitem", { name: /^Sent via act-alert-view, 1 alert/ });
    await expect(item).toHaveCount(1);
    await expect(item.locator("time")).toBeVisible();
    await expect(item.getByText("Sent", { exact: true })).toBeVisible();
    // On the alert inspector the row IS the alert, so it names the route that
    // paged rather than repeating the alert line.
    await expect(item.getByText("notif-alert-view", { exact: true })).toBeVisible();
  });
});
