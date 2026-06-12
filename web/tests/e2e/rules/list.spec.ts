// web/tests/e2e/rules/list.spec.ts
//
// Drives the RulesPage at /web/rules:
//   - Tab switch (Rules ↔ Aggregates).
//   - Empty state.
//   - Topbar count.
//   - Tree-order rendering for nested rules.
//   - Server-side sort on the aggregates tab.
//
// Note: the rules tab uses RulesTreeTable (DnD-sortable, ordered by
// tree_order). Sort-by-column is now an aggregates-tab feature only —
// rules order is operator-controlled via drag-and-drop / the rule editor.
import { test, expect } from "../harness/fixtures";

test.describe("rules list", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.rules.clear();
    await api.aggregaterules.clear();
    await adminAuth();
  });

  test("empty state renders when no rules exist", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/rules");
    // RulesTreeTable empty state — verbatim from RulesTreeTable.tsx (no trailing period).
    await expect(page.getByText("No rules yet")).toBeVisible();
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

  test("rule tree renders in tree_order ascending", async ({ page, api, server }) => {
    // Insert out of declaration order; the tree component sorts each level
    // by tree_order ASC (then name as tiebreak).
    await api.rules.create({
      name: "third",
      tree_order: 2,
      condition: { type: "ALWAYS_TRUE" },
    });
    await api.rules.create({
      name: "first",
      tree_order: 0,
      condition: { type: "ALWAYS_TRUE" },
    });
    await api.rules.create({
      name: "second",
      tree_order: 1,
      condition: { type: "ALWAYS_TRUE" },
    });
    await page.goto(server.baseURL + "/web/rules");

    const rows = page.getByRole("row");
    // RulesTreeTable renders its header with role=row, so nth(0) is the
    // header ("Name Condition Modifications"). Data rows start at nth(1).
    await expect(rows.nth(1)).toContainText("first");
    await expect(rows.nth(2)).toContainText("second");
    await expect(rows.nth(3)).toContainText("third");
  });

  test("aggregates tab still supports name column sort", async ({ page, api, server }) => {
    await api.aggregaterules.create({ name: "zulu-agg", condition: { type: "ALWAYS_TRUE" } });
    await api.aggregaterules.create({ name: "alpha-agg", condition: { type: "ALWAYS_TRUE" } });
    await page.goto(server.baseURL + "/web/rules");
    await page.getByRole("tab", { name: /aggregates/i }).click({ force: true });

    // Default sort: asc by name → alpha-agg first.
    const rows = page.getByRole("row");
    await expect(rows.nth(1)).toContainText("alpha-agg");

    // Click Name header → flips to descending.
    await page.getByRole("button", { name: /^name$/i }).click({ force: true });
    await expect(rows.nth(1)).toContainText("zulu-agg");
  });
});
