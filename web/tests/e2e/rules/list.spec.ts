// web/tests/e2e/rules/list.spec.ts
//
// Drives the RulesPage at /web/rules:
//   - Tab switch (Rules ↔ Aggregates).
//   - Empty state.
//   - Topbar count.
//   - Server-side sort by Name flips row order.
//
// Selectors come from:
//   - RulesPage.tsx       — Tabs.TabTrigger "Rules" / "Aggregates", "New" button
//   - columns.tsx         — column header "Name" (sortable button)
//   - DataTable           — default empty state "No items"
import { test, expect } from "../harness/fixtures";

test.describe("rules list", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.rules.clear();
    await api.aggregaterules.clear();
    await adminAuth();
  });

  test("empty state renders when no rules exist", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/rules");
    await expect(page.getByText("No items")).toBeVisible();
  });

  test("rules appear in the table after API create", async ({ page, api, server }) => {
    await api.rules.create({
      name: "alpha-rule",
      enabled: true,
      condition: { type: "ALWAYS_TRUE" },
    });
    await api.rules.create({
      name: "beta-rule",
      enabled: false,
      condition: { type: "ALWAYS_TRUE" },
    });
    await page.goto(server.baseURL + "/web/rules");
    await expect(page.getByText("alpha-rule")).toBeVisible();
    await expect(page.getByText("beta-rule")).toBeVisible();
    // Topbar count
    await expect(page.getByText(/2 rules/i)).toBeVisible();
  });

  test("tab switch to Aggregates shows the aggregate list", async ({ page, api, server }) => {
    await api.rules.create({ name: "rule-only", condition: { type: "ALWAYS_TRUE" } });
    await api.aggregaterules.create({
      name: "agg-only",
      condition: { type: "ALWAYS_TRUE" },
    });
    await page.goto(server.baseURL + "/web/rules");
    await expect(page.getByText("rule-only")).toBeVisible();
    // The Aggregates tab is a Radix Tabs.Trigger; clicking switches lists.
    await page.getByRole("tab", { name: /aggregates/i }).click({ force: true });
    await expect(page.getByText("agg-only")).toBeVisible();
    await expect(page.getByText("rule-only")).toBeHidden();
  });

  test("name column sort flips ascending vs descending", async ({ page, api, server }) => {
    await api.rules.create({ name: "zulu-rule", condition: { type: "ALWAYS_TRUE" } });
    await api.rules.create({ name: "alpha-rule", condition: { type: "ALWAYS_TRUE" } });
    await page.goto(server.baseURL + "/web/rules");
    await expect(page.getByText("alpha-rule")).toBeVisible();

    // Default sort: asc by name → alpha-rule first.
    const rows = page.getByRole("row");
    await expect(rows.nth(1)).toContainText("alpha-rule");

    // Click Name header → flips to descending.
    await page.getByRole("button", { name: /^name$/i }).click({ force: true });
    await expect(rows.nth(1)).toContainText("zulu-rule");
  });
});
