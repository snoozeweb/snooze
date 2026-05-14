import { test, expect } from "../harness/fixtures";

test.describe("alerts auto-refresh", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.alerts.clear();
    await adminAuth();
  });

  test("auto-refresh is on by default, new alert appears without manual reload", async ({
    page,
    api,
    server,
  }) => {
    await page.goto(server.baseURL + "/web/alerts");

    // The Switch has aria-label="Auto refresh" and defaults to enabled
    // (useAutoRefresh reads localStorage "alerts.autoRefresh", defaults true when key absent)
    const sw = page.getByRole("switch", { name: /auto refresh/i });
    await expect(sw).toBeVisible();
    // Default should be checked (enabled)
    await expect(sw).toHaveAttribute("data-state", "checked");

    // Inject a new alert via API while the page is live
    await api.alerts.send({ host: "srv-autorefresh", message: "m", severity: "info", source: "t" });

    // The page polls every 5s; wait up to 15s for the row to appear
    await expect(page.getByText("srv-autorefresh")).toBeVisible({ timeout: 15_000 });
  });

  test("toggle off, new alert does NOT appear within poll window", async ({
    page,
    api,
    server,
  }) => {
    await page.goto(server.baseURL + "/web/alerts");

    const sw = page.getByRole("switch", { name: /auto refresh/i });
    await expect(sw).toBeVisible();

    // Turn off if currently on
    if ((await sw.getAttribute("data-state")) === "checked") {
      await sw.click();
    }
    await expect(sw).toHaveAttribute("data-state", "unchecked");

    // Seed an alert after disabling
    await api.alerts.send({ host: "srv-hidden", message: "m", severity: "info", source: "t" });

    // Wait 3s — no poll should have fired
    await page.waitForTimeout(3_000);
    await expect(page.getByText("srv-hidden")).toBeHidden();
  });

  test("toggle switch changes state and persists label", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/alerts");
    const sw = page.getByRole("switch", { name: /auto refresh/i });
    await expect(sw).toBeVisible();

    // Read current state
    const initialState = await sw.getAttribute("data-state");

    // Toggle
    await sw.click();
    const newState = initialState === "checked" ? "unchecked" : "checked";
    await expect(sw).toHaveAttribute("data-state", newState);

    // Toggle back
    await sw.click();
    await expect(sw).toHaveAttribute("data-state", initialState ?? "checked");
  });
});
