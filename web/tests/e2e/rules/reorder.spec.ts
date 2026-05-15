// web/tests/e2e/rules/reorder.spec.ts
//
// Exercises the data path the drag-and-drop reorder relies on:
// PATCH /api/v1/rule/{uid} with {tree_order: N} updates the on-screen
// order. The drag mechanics themselves (PointerSensor, KeyboardSensor)
// are covered by the RulesTreeTable.test.tsx vitest pack — driving them
// from headless Playwright is brittle, but the wire shape is what
// matters here: a reorder issues PATCHes, the list re-fetches, the
// tree re-renders in the new order.
import { test, expect } from "../harness/fixtures";

test.describe("rules tree reorder", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.rules.clear();
    await adminAuth();
  });

  test("PATCHing tree_order reorders the tree and persists across reload", async ({
    page,
    api,
    server,
  }) => {
    const a = await api.rules.create({
      name: "alpha",
      tree_order: 0,
      condition: { type: "ALWAYS_TRUE" },
    });
    const b = await api.rules.create({
      name: "bravo",
      tree_order: 1,
      condition: { type: "ALWAYS_TRUE" },
    });
    const c = await api.rules.create({
      name: "charlie",
      tree_order: 2,
      condition: { type: "ALWAYS_TRUE" },
    });

    await page.goto(server.baseURL + "/web/rules");
    const rows = page.getByRole("row");
    await expect(rows.nth(0)).toContainText("alpha");
    await expect(rows.nth(1)).toContainText("bravo");
    await expect(rows.nth(2)).toContainText("charlie");

    // PATCH each sibling with a new tree_order, identical to what the
    // RulesTreeTable's onDragEnd handler dispatches.
    const headers = { Authorization: `Bearer ${api.token}` };
    await api.ctx.patch(`${server.baseURL}/api/v1/rule/${a.uid}`, {
      headers,
      data: { tree_order: 2 },
    });
    await api.ctx.patch(`${server.baseURL}/api/v1/rule/${b.uid}`, {
      headers,
      data: { tree_order: 0 },
    });
    await api.ctx.patch(`${server.baseURL}/api/v1/rule/${c.uid}`, {
      headers,
      data: { tree_order: 1 },
    });

    await page.reload();
    await expect(rows.nth(0)).toContainText("bravo");
    await expect(rows.nth(1)).toContainText("charlie");
    await expect(rows.nth(2)).toContainText("alpha");
  });
});
