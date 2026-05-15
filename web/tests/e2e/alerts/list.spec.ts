import { test, expect } from "../harness/fixtures";

test.describe("alerts list", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.alerts.clear();
    await adminAuth();
  });

  test("empty state when no alerts", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/alerts");
    // DataTable default emptyState renders EmptyState with title="No items"
    await expect(page.getByText("No items")).toBeVisible();
  });

  test("ingested alert appears in the table", async ({ page, api, server }) => {
    await api.alerts.send({
      host: "srv-1",
      message: "disk full",
      severity: "critical",
      source: "test",
    });
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("srv-1")).toBeVisible();
    await expect(page.getByText("disk full")).toBeVisible();
  });

  test("severity filter narrows results", async ({ page, api, server }) => {
    await api.alerts.sendMany([
      { host: "h1", message: "msg-a", severity: "info", source: "t" },
      { host: "h2", message: "msg-b", severity: "critical", source: "t" },
    ]);
    await page.goto(server.baseURL + "/web/alerts");
    // Wait for both rows
    await expect(page.getByText("h1")).toBeVisible();
    await expect(page.getByText("h2")).toBeVisible();

    // Severity filter is a <Select> with placeholder "Severity"
    // SelectTrigger renders as a button
    await page.getByRole("combobox").filter({ hasText: /severity/i }).click({ force: true });
    // Select "Critical" from the dropdown
    await page.getByRole("option", { name: /^critical$/i }).click({ force: true });

    await expect(page.getByText("h2")).toBeVisible();
    await expect(page.getByText("h1")).toBeHidden();
  });

  test("column sort by Host flips row order", async ({ page, api, server }) => {
    await api.alerts.sendMany([
      { host: "alpha-host", message: "x", severity: "info", source: "t" },
      { host: "zulu-host", message: "y", severity: "info", source: "t" },
    ]);
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("alpha-host")).toBeVisible();

    // Column header "Host" renders as a sortable button in the thead
    const hostHeader = page.getByRole("button", { name: /^host$/i });
    await hostHeader.click({ force: true });
    // asc: alpha should come first
    const rows = page.getByRole("row");
    await expect(rows.nth(1)).toContainText("alpha-host");

    // Click again: desc — zulu should come first
    await hostHeader.click({ force: true });
    await expect(rows.nth(1)).toContainText("zulu-host");
  });

  test("topbar shows alert count", async ({ page, api, server }) => {
    await api.alerts.sendMany([
      { host: "c1", message: "m", severity: "info", source: "t" },
      { host: "c2", message: "m", severity: "info", source: "t" },
    ]);
    await page.goto(server.baseURL + "/web/alerts");
    // Topbar shows "N alerts"
    await expect(page.getByText(/2 alerts/i)).toBeVisible();
  });
});
