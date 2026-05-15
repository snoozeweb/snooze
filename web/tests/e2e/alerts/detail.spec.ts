import { test, expect } from "../harness/fixtures";

test.describe("alert detail drawer", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.alerts.clear();
    await adminAuth();
  });

  test("opens drawer with full field map when row is clicked", async ({ page, api, server }) => {
    await api.alerts.send({
      host: "srv-detail",
      message: "disk full",
      severity: "critical",
      source: "prom",
    });
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("srv-detail")).toBeVisible();

    // Clicking the row calls onRowOpen → URL gets ?uid=... → DrawerContent renders
    // Radix Drawer renders with role="dialog"
    await page.getByText("srv-detail").first().click({ force: true });
    const drawer = page.getByRole("dialog");
    await expect(drawer).toBeVisible();

    // DrawerTitle shows host, body shows severity badge and message
    await expect(drawer.getByText("disk full")).toBeVisible();
    await expect(drawer.getByText("critical")).toBeVisible();
    await expect(drawer.getByText("prom")).toBeVisible();
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

    // After confirmation, the state badge in the table should show "Acknowledged"
    await expect(page.getByText("Acknowledged")).toBeVisible();
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

    // Now open the detail drawer to see the timeline
    await page.getByText("srv-comment").first().click({ force: true });
    const drawer = page.getByRole("dialog");
    await expect(drawer).toBeVisible();
    await expect(drawer.getByText("first note")).toBeVisible();
  });

  test("detail drawer closes when navigating away", async ({ page, api, server }) => {
    await api.alerts.send({ host: "srv-close-drawer", message: "m", severity: "info", source: "t" });
    await page.goto(server.baseURL + "/web/alerts");
    await page.getByText("srv-close-drawer").first().click({ force: true });

    const drawer = page.getByRole("dialog");
    await expect(drawer).toBeVisible();

    // Pressing Escape closes the drawer (Radix Drawer handles this natively)
    await page.keyboard.press("Escape");
    await expect(drawer).not.toBeVisible();
  });
});
