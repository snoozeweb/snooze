import { test, expect } from "../harness/fixtures";

test.describe("alert detail drawer", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.alerts.clear();
    await adminAuth();
  });

  test("expanded row shows the alert's full field map", async ({ page, api, server }) => {
    await api.alerts.send({
      host: "srv-detail",
      message: "disk full",
      severity: "critical",
      source: "prom",
    });
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("srv-detail")).toBeVisible();

    // AlertsPage uses DataTable's renderExpanded (AlertRowDetail), not a
    // Drawer. Each row has a chevron button labelled "Expand row <key>" that
    // toggles an inline panel showing the JsonViewer + CommentTimeline.
    await page.getByRole("button", { name: /^expand row/i }).first().click({ force: true });

    // The inline panel renders the full record via JsonViewer; the host /
    // message / severity / source values all appear as text nodes. Use
    // .first() because JsonViewer renders each field twice — once in the
    // table column (plain) and once in the JSON tree (quoted span).
    await expect(page.getByText("disk full").first()).toBeVisible();
    await expect(page.getByText("critical").first()).toBeVisible();
    await expect(page.getByText("prom").first()).toBeVisible();
  });

  test("ack action changes alert state to Acknowledged via row actions menu", async ({
    page,
    api,
    server,
  }) => {
    await api.alerts.send({ host: "srv-ack", message: "m", severity: "info", source: "t" });
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("srv-ack")).toBeVisible();

    // Row actions menu: click the "..." button for the row
    await page.getByRole("button", { name: /row actions/i }).first().click({ force: true });
    await page.getByRole("menuitem", { name: /acknowledge/i }).click({ force: true });

    // ActionDialog opens with title "Acknowledge alert"
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("heading", { name: /acknowledge alert/i })).toBeVisible();

    // Confirm
    await dialog.getByRole("button", { name: /^acknowledge$/i }).click({ force: true });

    // After confirmation, the state badge in the table should show "Ack"
    // (shortened from the previous "Acknowledged" label so the State column
    // doesn't grow under typical row content). Reload to give the records
    // query time to re-fetch; the action dialog doesn't always invalidate
    // tightly in headless CDP under WSL.
    await page.reload();
    await expect(page.getByText("Ack", { exact: true })).toBeVisible();
  });

  test("close action moves alert to Closed state", async ({ page, api, server }) => {
    await api.alerts.send({ host: "srv-close", message: "m", severity: "info", source: "t" });
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("srv-close")).toBeVisible();

    await page.getByRole("button", { name: /row actions/i }).first().click({ force: true });
    await page.getByRole("menuitem", { name: /^close$/i }).click({ force: true });

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: /^close$/i }).click({ force: true });

    // After closing, state badge shows "Closed"
    await expect(page.getByText("Closed")).toBeVisible();
  });

  test("comment appears in timeline after posting via row actions menu", async ({
    page,
    api,
    server,
  }) => {
    await api.alerts.send({ host: "srv-comment", message: "m", severity: "info", source: "t" });
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("srv-comment")).toBeVisible();

    await page.getByRole("button", { name: /row actions/i }).first().click({ force: true });
    await page.getByRole("menuitem", { name: /comment/i }).click({ force: true });

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();

    // The Comment action dialog has requireMessage: true — placeholder is "Type your comment"
    const textarea = dialog.getByPlaceholder("Type your comment");
    await textarea.fill("first note");
    await dialog.getByRole("button", { name: /^comment$/i }).click({ force: true });

    // Expand the row inline (no drawer here) and check the timeline.
    await page.getByRole("button", { name: /^expand row/i }).first().click({ force: true });
    await expect(page.getByText("first note")).toBeVisible();
  });

  test("expanded row collapses when chevron is toggled again", async ({ page, api, server }) => {
    await api.alerts.send({ host: "srv-close-drawer", message: "m", severity: "info", source: "t" });
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("srv-close-drawer")).toBeVisible();

    const expandBtn = page.getByRole("button", { name: /^expand row/i }).first();
    await expandBtn.click({ force: true });
    await expect(expandBtn).toHaveAttribute("aria-expanded", "true");

    // Clicking again collapses the inline panel.
    await expandBtn.click({ force: true });
    await expect(expandBtn).toHaveAttribute("aria-expanded", "false");
  });
});
