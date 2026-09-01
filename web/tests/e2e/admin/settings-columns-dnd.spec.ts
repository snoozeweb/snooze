// web/tests/e2e/admin/settings-columns-dnd.spec.ts
//
// Drives the Console settings tab and verifies the "Alert table columns"
// setting can be reordered by dragging the per-row grip handle. Emulates a
// real pointer drag (dnd-kit's PointerSensor activates after >6px of motion),
// dragging the first row down past the second row's midpoint.
import { test, expect } from "../harness/fixtures";

test.describe("admin / settings — Alert table columns drag reorder", () => {
  test.beforeEach(async ({ adminAuth }) => {
    await adminAuth();
  });

  test("grip handle reorders the column rows", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/admin/settings?tab=console");

    // The card is the <section> that owns the "Alert table columns" label.
    const card = page
      .locator("section")
      .filter({ has: page.getByText("Alert table columns", { exact: true }) });
    await expect(card).toBeVisible();

    const inputs = card.getByRole("textbox");
    const handles = card.getByRole("button", { name: "Drag to reorder" });

    // Default catalogue value seeds the message-first column order
    // (internal/pluginimpl/settings/metadata.yaml `columns`).
    await expect(inputs.first()).toHaveValue("severity");
    await expect(handles).toHaveCount(await inputs.count());
    const before = await inputs.evaluateAll((els) =>
      els.map((e) => (e as HTMLInputElement).value),
    );
    expect(before.length).toBeGreaterThan(2);

    // Pointer drag: grab the first row's handle and drag it down past the
    // second row's midpoint so the two swap.
    const from = await handles.first().boundingBox();
    const to = await inputs.nth(1).boundingBox();
    if (!from || !to) throw new Error("could not measure drag handle / target row");

    await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
    await page.mouse.down();
    // Exceed the 6px PointerSensor activation threshold.
    await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2 + 12, { steps: 4 });
    // Land below the second row's centre so it sorts past it.
    await page.mouse.move(to.x + to.width / 2, to.y + to.height * 0.75, { steps: 8 });
    await page.mouse.up();

    const after = await inputs.evaluateAll((els) =>
      els.map((e) => (e as HTMLInputElement).value),
    );

    // First two entries swap; everything else is untouched.
    const expected = [before[1], before[0], ...before.slice(2)];
    expect(after).toEqual(expected);

    // The reorder marks the card dirty; Save persists it and it survives a
    // reload (the setting is stored as an ordered array).
    const save = card.getByRole("button", { name: "Save" });
    await expect(save).toBeEnabled();
    await save.click();

    await page.goto(server.baseURL + "/web/admin/settings?tab=console");
    await expect(card).toBeVisible();
    await expect(inputs.first()).toHaveValue(expected[0]!);
    const persisted = await inputs.evaluateAll((els) =>
      els.map((e) => (e as HTMLInputElement).value),
    );
    expect(persisted).toEqual(expected);
  });
});
