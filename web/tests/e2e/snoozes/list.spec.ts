// web/tests/e2e/snoozes/list.spec.ts
//
// Drives SnoozesPage at /web/snoozes. Covers:
//   - Empty state.
//   - Snoozes appear after API create.
//   - TTL column renders "forever" for ttl=0 and a relative label for ttl>0.
//   - Topbar count matches DB state.
import { test, expect } from "../harness/fixtures";

test.describe("snoozes list", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.snoozes.clear();
    await adminAuth();
  });

  test("empty state when no snoozes", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/snoozes");
    await expect(page.getByText("No items")).toBeVisible();
  });

  test("snoozes appear after API create, topbar counts them", async ({ page, api, server }) => {
    await api.snoozes.create({
      name: "snooze-forever",
      enabled: true,
      condition: { type: "ALWAYS_TRUE" },
      ttl: 0,
    });
    await api.snoozes.create({
      name: "snooze-1h",
      enabled: true,
      condition: { type: "ALWAYS_TRUE" },
      ttl: 3600,
    });
    await page.goto(server.baseURL + "/web/snoozes");
    await expect(page.getByText("snooze-forever")).toBeVisible();
    await expect(page.getByText("snooze-1h")).toBeVisible();
    // TTL column renders "forever" for ttl=0 and "1h" for ttl=3600.
    await expect(page.getByText("forever").first()).toBeVisible();
    await expect(page.getByText("1h").first()).toBeVisible();
    // Topbar pluralised count.
    await expect(page.getByText(/2 snoozes/i)).toBeVisible();
  });
});
