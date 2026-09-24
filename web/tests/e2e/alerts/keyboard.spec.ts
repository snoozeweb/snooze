// Phase 6 — power-user flow: row traversal from the keyboard and the search
// query staying legible while a bulk action is committed against it.
import { test, expect } from "../harness/fixtures";

test.describe("alerts keyboard traversal", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.alerts.clear();
    await adminAuth();
  });

  test("j / k move the focus ring and Enter opens the focused row's drawer", async ({
    page,
    api,
    server,
  }) => {
    // Two rows, newest first: kb-two is row 1, kb-one is row 2.
    await api.alerts.send({ host: "kb-one", message: "first alert", severity: "info" });
    await api.alerts.send({ host: "kb-two", message: "second alert", severity: "info" });
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("kb-one")).toBeVisible();
    await expect(page.getByText("kb-two")).toBeVisible();

    const grid = page.getByRole("grid");
    await grid.focus();

    // j → first row focused. The grid names it via aria-activedescendant, so
    // the ring has a spoken counterpart.
    await page.keyboard.press("j");
    const firstRow = page.locator("tr[data-focused='true']");
    await expect(firstRow).toHaveCount(1);
    const firstId = await firstRow.getAttribute("id");
    expect(await grid.getAttribute("aria-activedescendant")).toBe(firstId);

    // j again → second row; k → back to the first.
    await page.keyboard.press("j");
    const secondId = await page.locator("tr[data-focused='true']").getAttribute("id");
    expect(secondId).not.toBe(firstId);
    await page.keyboard.press("k");
    expect(await page.locator("tr[data-focused='true']").getAttribute("id")).toBe(firstId);

    // Enter opens the detail drawer on the focused row, and the URL carries it.
    await page.keyboard.press("Enter");
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(page).toHaveURL(/record=/);

    // The drawer's own j/k page to the next/previous row without touching the
    // mouse — the position counter is the proof.
    await expect(dialog.getByText("1 / 2")).toBeVisible();
    await page.keyboard.press("j");
    await expect(dialog.getByText("2 / 2")).toBeVisible();
    await page.keyboard.press("k");
    await expect(dialog.getByText("1 / 2")).toBeVisible();
  });

  test("a on the focused row asks to confirm before acknowledging", async ({
    page,
    api,
    server,
  }) => {
    await api.alerts.send({ host: "kb-ack", message: "needs an ack", severity: "info" });
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("kb-ack")).toBeVisible();

    await page.getByRole("grid").focus();
    await page.keyboard.press("j");
    await page.keyboard.press("a");

    // Never skip the confirm: a keystroke lands on whichever row the ring is
    // on, so it goes through the same dialog the kebab uses.
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText(/acknowledge alert/i)).toBeVisible();
  });

  test("Space selects the focused row and Escape clears the selection", async ({
    page,
    api,
    server,
  }) => {
    await api.alerts.send({ host: "kb-select", message: "selectable", severity: "info" });
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("kb-select")).toBeVisible();

    await page.getByRole("grid").focus();
    await page.keyboard.press("j");
    await page.keyboard.press(" ");
    await expect(page.getByText("1 selected")).toBeVisible();

    await page.keyboard.press("Escape");
    await expect(page.getByText("1 selected")).toHaveCount(0);
  });

  test("? opens the shortcut legend", async ({ page, api, server }) => {
    await api.alerts.send({ host: "kb-help", message: "help", severity: "info" });
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("kb-help")).toBeVisible();

    await page.getByRole("grid").focus();
    await page.keyboard.press("?");
    await expect(page.getByText("Move between rows")).toBeVisible();
    await expect(page.getByText("Acknowledge focused alert")).toBeVisible();
  });
});

test.describe("alerts bulk bar keeps the query visible", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.alerts.clear();
    await adminAuth();
  });

  test("the search box still shows the query that produced the selection", async ({
    page,
    api,
    server,
  }) => {
    await api.alerts.sendMany([
      { host: "bulkq-keep", message: "disk pressure", severity: "info", source: "t" },
      { host: "bulkq-other", message: "network flap", severity: "info", source: "t" },
    ]);
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("bulkq-keep")).toBeVisible();

    const search = page.getByRole("textbox", { name: /^search$/i });
    await search.fill('host = "bulkq-keep"');
    // Escape dismisses the autocomplete popover so Enter commits the query
    // instead of picking a suggestion (see search-url.spec.ts).
    await search.press("Escape");
    await search.press("Enter");
    await expect(page.getByText("bulkq-other")).toHaveCount(0);

    await page.getByRole("checkbox", { name: /select all/i }).check({ force: true });
    await expect(page.getByText("1 selected")).toBeVisible();

    // The bulk bar is its own row; the query that produced the selection is
    // still on screen, still readable, and still editable at commit time.
    await expect(search).toHaveValue('host = "bulkq-keep"');
    await expect(search).toBeVisible();
    const box = await search.boundingBox();
    expect(box?.width ?? 0).toBeGreaterThan(200);
  });
});
