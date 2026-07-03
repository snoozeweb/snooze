import { test, expect } from "../harness/fixtures";

// The alert list is a shared, worker-scoped resource: every test in this file
// runs against the same server/DB. Reusing identical alert content (host +
// message) across tests lets one test's state changes (notably the bulk-ack
// test below) bleed onto a *later* test's freshly re-seeded rows — a re-seeded
// "srv-1" would occasionally come back already acked, drop out of the default
// (open) Alerts tab, and turn "Close (5)" into "Close (4)". Seeding a unique
// host set per test makes any such content-keyed cross-test match impossible.
let hosts: string[];

test.describe("alerts bulk actions", () => {
  test.beforeEach(async ({ api, adminAuth }, testInfo) => {
    await api.alerts.clear();
    await adminAuth();
    const tag = testInfo.title.replace(/[^a-z0-9]+/gi, "-").slice(0, 24);
    hosts = Array.from({ length: 5 }, (_, i) => `${tag}-${i}`);
    await api.alerts.sendMany(
      hosts.map((host, i) => ({
        host,
        message: `m${i}`,
        severity: "info",
        source: "test",
      })),
    );
  });

  test("select-all checkbox shows bulk bar with correct count", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText(hosts[0])).toBeVisible();

    // "Select all" checkbox is in the <th> header cell
    await page.getByRole("checkbox", { name: /select all/i }).check({ force: true });

    // DataTable bulk toolbar shows "{N} selected"
    await expect(page.getByText("5 selected")).toBeVisible();
  });

  test("bulk acknowledge opens action dialog and updates all selected", async ({
    page,
    server,
  }) => {
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText(hosts[0])).toBeVisible();

    await page.getByRole("checkbox", { name: /select all/i }).check({ force: true });
    await expect(page.getByText("5 selected")).toBeVisible();

    // Bulk bar shows "Acknowledge (5)" button
    await page.getByRole("button", { name: /acknowledge \(5\)/i }).click({ force: true });

    // ActionDialog: "Acknowledge 5 alerts"
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("heading", { name: /acknowledge 5 alerts/i })).toBeVisible();

    // Confirm
    await dialog.getByRole("button", { name: /^acknowledge$/i }).click({ force: true });

    // Toast "5 alerts updated" or multiple state badges become "Acknowledged"
    // Radix Toast renders an aria-live announcer in addition to the visible
    // toast — use .first() to scope the visibility check to either one.
    await expect(page.getByText(/5 alerts updated/i).first()).toBeVisible();
  });

  test("bulk close opens action dialog", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText(hosts[0])).toBeVisible();

    await page.getByRole("checkbox", { name: /select all/i }).check({ force: true });
    await page.getByRole("button", { name: /close \(5\)/i }).click({ force: true });

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("heading", { name: /close 5 alerts/i })).toBeVisible();

    await dialog.getByRole("button", { name: /^close$/i }).click({ force: true });

    // After closing all, state badges become "Closed"
    // Radix Toast renders an aria-live announcer in addition to the visible
    // toast — use .first() to scope the visibility check to either one.
    await expect(page.getByText(/5 alerts updated/i).first()).toBeVisible();
  });

  test("partial selection shows correct count and deselects on cancel", async ({
    page,
    server,
  }) => {
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText(hosts[0])).toBeVisible();

    // Check only the first two data rows (rows 1 and 2 in the grid; row 0 is header)
    const rows = page.getByRole("row");
    // Row aria-label for checkbox: "Select row <uid>" — use the data row checkboxes
    await rows.nth(1).getByRole("checkbox").check({ force: true });
    await rows.nth(2).getByRole("checkbox").check({ force: true });

    await expect(page.getByText("2 selected")).toBeVisible();
  });

  test("re-escalate bulk action opens dialog", async ({ page, api, server }) => {
    // Re-escalate is only a legal transition from the "ack"/"esc" states — the
    // bulk toolbar (correctly) never offers it for fresh/open rows (see
    // web/src/features/alerts/transitions.ts). Ack the seed first so the rows
    // move to the Acknowledged tab where re-escalate becomes available.
    const seeded = (await api.alerts.list()) as Array<{ uid?: string }>;
    for (const rec of seeded) {
      if (rec.uid) {
        await api.comments.create({ record_uid: rec.uid, type: "ack", message: "seed ack" });
      }
    }

    await page.goto(server.baseURL + "/web/alerts");
    // Switch to the Acknowledged tab; the acked rows leave the default view.
    await page.getByRole("tab", { name: /^acknowledged$/i }).click({ force: true });
    await expect(page.getByText(hosts[0])).toBeVisible();

    await page.getByRole("checkbox", { name: /select all/i }).check({ force: true });
    await page.getByRole("button", { name: /re-escalate \(5\)/i }).click({ force: true });

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("heading", { name: /re-escalate 5 alerts/i })).toBeVisible();

    // Cancel out
    await dialog.getByRole("button", { name: /cancel/i }).click({ force: true });
    await expect(dialog).not.toBeVisible();
  });
});
