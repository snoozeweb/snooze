// web/tests/e2e/notifications/deliveries.spec.ts
//
// Delivery history (the `notificationlog` collection) end-to-end, against a
// real server: an alert is injected, the dispatcher actually runs a `script`
// action, a row lands in the log, and the notifications page shows it.
//
// Everything here is timing-sensitive in one specific way: the ingest POST
// returns BEFORE anything has been sent. `notification.Process` spawns a
// detached coordinator goroutine that runs the notifier, resolves the record
// uid and only then writes the delivery row — so every assertion waits on
// `api.waitForDeliveries()` first and drives the UI second. Batched actions
// defer further still (the row is written at flush, not at queue).
import { test, expect } from "../harness/fixtures";
import type { Page } from "@playwright/test";
import type { DeliveryLogRow, SnoozeApi } from "../harness/api";

/** Creates one `script` action plus a notification routing `source` to it. */
async function seedRoute(
  api: SnoozeApi,
  opts: {
    actionName: string;
    notifName: string;
    source: string;
    subcontent: Record<string, unknown>;
  },
): Promise<{ notificationUid: string }> {
  await api.actions.create({
    name: opts.actionName,
    action: { selected: "script", subcontent: opts.subcontent },
  });
  const notification = await api.notifications.create({
    name: opts.notifName,
    enabled: true,
    condition: { type: "=", field: "source", value: opts.source },
    actions: [opts.actionName],
  });
  return { notificationUid: notification.uid };
}

/** Opens the row inspector from a table row's hover-revealed eye button. */
async function openDetails(page: Page, rowText: string) {
  await expect(page.locator("tr", { hasText: rowText }).first()).toBeVisible();
  await page
    .locator("tr", { hasText: rowText })
    .first()
    .getByRole("button", { name: "View details" })
    .click({ force: true });
  const drawer = page.getByRole("dialog");
  await expect(drawer).toBeVisible();
  return drawer;
}

test.describe("delivery history", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.notifications.clear();
    await api.actions.clear();
    await api.alerts.clear();
    await api.notificationlog.clear();
    await adminAuth();
  });

  test("a successful send is listed on the notification's Deliveries tab", async ({
    page,
    api,
    server,
  }) => {
    const { notificationUid } = await seedRoute(api, {
      actionName: "act-ok",
      notifName: "notif-ok",
      source: "e2e-ok",
      subcontent: { command: ["/bin/true"] },
    });
    await api.warmUpDispatcher();

    await api.alerts.send({
      host: "srv-deliv-ok",
      message: "disk 98% on /var",
      severity: "critical",
      source: "e2e-ok",
    });
    const rows = await api.waitForDeliveries(1);
    expect(rows).toHaveLength(1);
    expect(rows[0]?.action).toBe("act-ok");
    expect(rows[0]?.status).toBe("success");

    await page.goto(server.baseURL + "/web/notifications");
    const drawer = await openDetails(page, "notif-ok");

    // The inspector is addressable — a dashboard deep link and a pasted URL
    // both land here.
    await expect(page).toHaveURL(new RegExp(`details=${notificationUid}`));

    // Deliveries leads: "did anyone actually get paged?" is the question that
    // brings an operator to a notification's details.
    await expect(drawer.getByRole("tab", { name: "Deliveries" })).toHaveAttribute(
      "aria-selected",
      "true",
    );

    // Summary strip counters (server-stamped `hits` / `last_sent`).
    await expect(drawer.getByText("Sent 1×")).toBeVisible();

    // One row, named as a sentence for screen readers.
    const item = drawer.getByRole("listitem", { name: /^Sent via act-ok, 1 alert/ });
    await expect(item).toHaveCount(1);
    await expect(item.locator("time")).toBeVisible();
    await expect(item.getByText("Sent", { exact: true })).toBeVisible();
    await expect(item.getByText("act-ok", { exact: true })).toBeVisible();

    // The alert that was in the send, with its severity, host and message.
    const alertLink = item.getByRole("link", { name: /^Open alert:/ });
    await expect(alertLink).toContainText("srv-deliv-ok");
    await expect(alertLink).toContainText("disk 98% on /var");
  });

  test("the alert line opens that alert in the alerts inspector", async ({ page, api, server }) => {
    await seedRoute(api, {
      actionName: "act-link",
      notifName: "notif-link",
      source: "e2e-link",
      subcontent: { command: ["/bin/true"] },
    });
    await api.warmUpDispatcher();

    await api.alerts.send({
      host: "srv-deliv-link",
      message: "link me",
      severity: "warning",
      source: "e2e-link",
    });
    const rows = await api.waitForDeliveries(1);
    // The coordinator resolves the record uid after the pipeline's write
    // lands, so the row addresses the alert directly rather than by hash.
    const alertUid = rows[0]?.alerts?.[0]?.uid;
    expect(alertUid, "delivery row should carry the alert uid").toBeTruthy();

    await page.goto(server.baseURL + "/web/notifications");
    const drawer = await openDetails(page, "notif-link");
    await drawer.getByRole("link", { name: /^Open alert:/ }).click({ force: true });

    await expect(page).toHaveURL(/\/web\/alerts\?/);
    await expect(page).toHaveURL(/tab=all/);
    await expect(page).toHaveURL(new RegExp(`record=${alertUid}`));

    const alertDrawer = page.getByRole("dialog");
    await expect(alertDrawer).toBeVisible();
    await expect(alertDrawer.getByText("srv-deliv-link").first()).toBeVisible();
  });

  test("a failed send shows Failed plus the error, and the chips narrow to it", async ({
    page,
    api,
    server,
  }) => {
    await seedRoute(api, {
      actionName: "act-fail",
      notifName: "notif-fail",
      source: "e2e-fail",
      subcontent: { command: ["/bin/false"] },
    });
    await api.warmUpDispatcher();

    await api.alerts.send({
      host: "srv-deliv-fail",
      message: "this one breaks",
      severity: "err",
      source: "e2e-fail",
    });
    const rows = await api.waitForDeliveries(1);
    expect(rows[0]?.status).toBe("error");
    expect(rows[0]?.error ?? "").not.toEqual("");

    await page.goto(server.baseURL + "/web/notifications");
    const drawer = await openDetails(page, "notif-fail");

    const item = drawer.getByRole("listitem", { name: /^Failed via act-fail/ });
    await expect(item).toHaveCount(1);
    await expect(item.getByText("Failed", { exact: true })).toBeVisible();
    // The error is the whole point of the row: it is shown verbatim, not
    // summarised into "something went wrong".
    await expect(item.locator("pre")).toContainText("script: exit 1");

    // A failed send never moves the counters (D10) — the strip still reads
    // "Never sent", which is a true statement about deliveries that landed.
    await expect(drawer.getByText("Never sent")).toBeVisible();

    const chips = drawer.getByRole("group", { name: "Filter deliveries" });
    await chips.getByRole("button", { name: "Failed" }).click({ force: true });
    await expect(drawer.getByRole("listitem", { name: /^Failed via act-fail/ })).toHaveCount(1);

    await chips.getByRole("button", { name: "Batched" }).click({ force: true });
    await expect(drawer.getByRole("listitem", { name: /^Failed via act-fail/ })).toHaveCount(0);
    await expect(drawer.getByText("No batched deliveries.")).toBeVisible();
  });

  test("a batched action writes one row per flush and links to every alert in it", async ({
    page,
    api,
    server,
  }) => {
    await seedRoute(api, {
      actionName: "act-batch",
      notifName: "notif-batch",
      source: "e2e-batch",
      subcontent: {
        command: ["/bin/true"],
        batch: true,
        batch_maxsize: 2,
        batch_timer: 60,
      },
    });
    await api.warmUpDispatcher();

    // Two alerts fill the bucket, so it flushes on size — not on the 60 s
    // timer, which is there precisely so a slow test can't flush by accident.
    await api.alerts.sendMany([
      {
        host: "srv-batch-a",
        message: "first of the batch",
        severity: "critical",
        source: "e2e-batch",
      },
      {
        host: "srv-batch-b",
        message: "second of the batch",
        severity: "warning",
        source: "e2e-batch",
      },
    ]);

    const rows = await api.waitForDeliveries(
      (r: DeliveryLogRow[]) => r.length > 0 && (r[0]?.alert_count ?? 0) >= 2,
    );
    // One flush = one row. Queue-time never writes a row (contract D8).
    expect(rows).toHaveLength(1);
    expect(rows[0]?.batch).toBe(true);
    expect(rows[0]?.batch_reason).toBe("size");
    expect(rows[0]?.alert_count).toBe(2);

    await page.goto(server.baseURL + "/web/notifications");
    const drawer = await openDetails(page, "notif-batch");

    const item = drawer.getByRole("listitem", { name: /^Sent via act-batch, 2 alerts/ });
    await expect(item).toHaveCount(1);
    await expect(item.getByText("Batch · 2 alerts")).toBeVisible();
    await expect(item.getByRole("link", { name: /^Open alert:/ })).toHaveCount(2);

    await item.getByRole("link", { name: /View all 2 alerts/ }).click({ force: true });

    await expect(page).toHaveURL(/\/web\/alerts\?/);
    // D12: the deep link carries the DSL the operator can see and edit, so it
    // has to land IN the SearchBar rather than in an opaque filter.
    const search = page.getByRole("textbox", { name: /^search$/i });
    await expect(search).toHaveValue(/uid IN/);
    await expect(page.locator("tr", { hasText: "srv-batch-a" })).toHaveCount(1);
    await expect(page.locator("tr", { hasText: "srv-batch-b" })).toHaveCount(1);
  });

  test("the table's Sent and Last sent columns follow the deliveries", async ({
    page,
    api,
    server,
  }) => {
    await seedRoute(api, {
      actionName: "act-cols",
      notifName: "notif-cols",
      source: "e2e-cols",
      subcontent: { command: ["/bin/true"] },
    });
    await api.warmUpDispatcher();

    await api.alerts.send({
      host: "srv-cols-1",
      message: "one",
      severity: "info",
      source: "e2e-cols",
    });
    await api.waitForDeliveries(1);

    await page.goto(server.baseURL + "/web/notifications");
    const row = page.locator("tr", { hasText: "notif-cols" }).first();
    await expect(row.locator('td[data-label="Sent"]')).toHaveText("1");
    await expect(row.locator('td[data-label="Last sent"] time')).toBeVisible();

    // A second, distinct alert is a second delivery — the counter is a count
    // of sends, not of notifications.
    await api.alerts.send({
      host: "srv-cols-2",
      message: "two",
      severity: "info",
      source: "e2e-cols",
    });
    await api.waitForDeliveries(2);
    await page.reload();
    await expect(
      page.locator("tr", { hasText: "notif-cols" }).first().locator('td[data-label="Sent"]'),
    ).toHaveText("2");
  });

  test("the Actions tab inspector lists the same delivery", async ({ page, api, server }) => {
    await seedRoute(api, {
      actionName: "act-inspect",
      notifName: "notif-inspect",
      source: "e2e-action",
      subcontent: { command: ["/bin/true"] },
    });
    await api.warmUpDispatcher();

    await api.alerts.send({
      host: "srv-action-tab",
      message: "from the action side",
      severity: "info",
      source: "e2e-action",
    });
    await api.waitForDeliveries(1);

    await page.goto(server.baseURL + "/web/notifications?tab=actions");
    const drawer = await openDetails(page, "act-inspect");
    await expect(drawer.getByRole("tab", { name: "Deliveries" })).toHaveAttribute(
      "aria-selected",
      "true",
    );

    const item = drawer.getByRole("listitem", { name: /^Sent via act-inspect, 1 alert/ });
    await expect(item).toHaveCount(1);
    // The action inspector drops the action badge (it IS the action) and
    // names the notification that routed the alert instead.
    await expect(item.getByText("notif-inspect", { exact: true })).toBeVisible();
    await expect(item.getByRole("link", { name: /^Open alert:/ })).toContainText("srv-action-tab");
  });

  test("?details= opens the inspector directly, and closing it drops the key", async ({
    page,
    api,
    server,
  }) => {
    const { notificationUid } = await seedRoute(api, {
      actionName: "act-deep",
      notifName: "notif-deep",
      source: "e2e-deep",
      subcontent: { command: ["/bin/true"] },
    });

    await page.goto(`${server.baseURL}/web/notifications?details=${notificationUid}`);
    const drawer = page.getByRole("dialog");
    await expect(drawer).toBeVisible();
    await expect(drawer.getByText("notif-deep").first()).toBeVisible();
    await expect(drawer.getByRole("tab", { name: "Deliveries" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    // Nothing has fired yet, and the empty state says why rows would appear
    // rather than just "no data".
    await expect(drawer.getByText(/No deliveries yet/i)).toBeVisible();

    await page.keyboard.press("Escape");
    await expect(drawer).toBeHidden();
    await expect(page).not.toHaveURL(/details=/);
  });
});
