// web/tests/e2e/snoozes/list.spec.ts
//
// Drives SnoozesPage at /web/snoozes. Covers:
//   - Empty state (no snoozes — Active tab shows "0 active snoozes").
//   - Snoozes without a datetime constraint land in the Active tab.
//   - Tab counts reflect snoozeState() — see web/src/features/snoozes/state.ts.
import { test, expect } from "../harness/fixtures";

test.describe("snoozes list", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.snoozes.clear();
    await adminAuth();
  });

  test("empty state when no snoozes", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/snoozes");
    // Default Active tab is selected; the topbar reports "0 active snoozes".
    await expect(page.getByText(/0 active snoozes/i)).toBeVisible();
  });

  test("snoozes without a datetime constraint appear under Active", async ({
    page,
    api,
    server,
  }) => {
    await api.snoozes.create({
      name: "snooze-forever",
      enabled: true,
      condition: { type: "ALWAYS_TRUE" },
    });
    await api.snoozes.create({
      name: "snooze-weekdays",
      enabled: true,
      condition: { type: "ALWAYS_TRUE" },
      time_constraints: { weekdays: [{ weekdays: [1, 2, 3, 4, 5] }] },
    });
    await page.goto(server.baseURL + "/web/snoozes");

    await expect(page.getByText("snooze-forever")).toBeVisible();
    await expect(page.getByText("snooze-weekdays")).toBeVisible();

    // Tab counts: 2 Active, 0 Upcoming, 0 Expired.
    await expect(page.getByRole("tab", { name: /active \(2\)/i })).toBeVisible();
    await expect(page.getByRole("tab", { name: /upcoming \(0\)/i })).toBeVisible();
    await expect(page.getByRole("tab", { name: /expired \(0\)/i })).toBeVisible();
    await expect(page.getByText(/2 active snoozes/i)).toBeVisible();
  });

  test("expired snooze appears under Expired tab", async ({ page, api, server }) => {
    const past = new Date(Date.now() - 86400 * 1000).toISOString();
    await api.snoozes.create({
      name: "snooze-expired",
      enabled: true,
      condition: { type: "ALWAYS_TRUE" },
      time_constraints: { datetime: [{ until: past }] },
    });
    await page.goto(server.baseURL + "/web/snoozes");
    // Active tab has 0; switch to Expired.
    await expect(page.getByRole("tab", { name: /expired \(1\)/i })).toBeVisible();
    await page.getByRole("tab", { name: /^expired/i }).click({ force: true });
    await expect(page.getByText("snooze-expired")).toBeVisible();
  });
});
