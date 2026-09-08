// web/tests/e2e/dashboard/deliveries.spec.ts
//
// The dashboard's Notifications breakdown: which routes actually sent in the
// window, and the deep link from one of them into its Deliveries tab
// pre-filtered to that same window.
//
// The panel is fed by the `notification_sent` counter, which only exists when
// `general.metrics_enabled` is on. The harness writes no `general.yaml`, so
// the default (true, internal/config/schema/general.go) applies — the same
// assumption dashboard/charts.spec.ts already makes.
import { test, expect } from "../harness/fixtures";

test.describe("dashboard notifications breakdown", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.notifications.clear();
    await api.actions.clear();
    await api.alerts.clear();
    await api.notificationlog.clear();
    await adminAuth();
  });

  test("the Notifications tab ranks routes and deep-links into their deliveries", async ({
    page,
    api,
    server,
  }) => {
    await api.actions.create({
      name: "act-dash",
      action: { selected: "script", subcontent: { command: ["/bin/true"] } },
    });
    const notification = await api.notifications.create({
      name: "notif-dash",
      enabled: true,
      condition: { type: "=", field: "source", value: "e2e-dash" },
      actions: ["act-dash"],
    });
    await api.warmUpDispatcher();

    await api.alerts.send({
      host: "srv-dash",
      message: "counted",
      severity: "critical",
      source: "e2e-dash",
    });
    await api.waitForDeliveries(1);

    // Counters are written through the async stats writer, so the panel can
    // lag the delivery row by a flush interval — poll the rendered page
    // rather than assuming one reload is enough.
    await page.goto(server.baseURL + "/web/dashboard");
    const tab = page.getByRole("tab", { name: "Notifications" });
    await expect(tab).toBeVisible();
    await expect
      .poll(
        async () => {
          await tab.click({ force: true });
          const n = await page.getByRole("link", { name: "notif-dash" }).count();
          if (n === 0) await page.reload();
          return n;
        },
        { timeout: 30_000 },
      )
      .toBeGreaterThan(0);

    await page.getByRole("link", { name: "notif-dash" }).click({ force: true });

    // Lands on the notification's inspector, scoped to the chart's window.
    await expect(page).toHaveURL(new RegExp(`details=${notification.uid}`));
    await expect(page).toHaveURL(/[?&]from=\d+/);
    await expect(page).toHaveURL(/[?&]to=\d+/);

    const drawer = page.getByRole("dialog");
    await expect(drawer).toBeVisible();
    await expect(drawer.getByRole("tab", { name: "Deliveries" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    // The window is a visible, dismissable chip — an inherited filter that
    // silently hid rows would be worse than no deep link at all.
    await expect(drawer.getByRole("button", { name: /^Remove window filter/ })).toBeVisible();
    await expect(drawer.getByRole("listitem", { name: /^Sent via act-dash, 1 alert/ })).toHaveCount(
      1,
    );
  });
});
