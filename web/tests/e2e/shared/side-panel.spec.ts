// Drawers in this app are side PANELS, not modals: the page a panel was opened
// from stays live beside it — the alerts inspector and every editor alike.
//
// It used to be a modal Radix dialog with a fixed full-screen scrim at
// --z-modal, so every control behind it was inert — paging to the next page
// with the inspector open did nothing at all, the click landing on the scrim.
import { test, expect } from "../harness/fixtures";
import { loginAsAdmin } from "../harness/auth";

const PAGE_SIZE = 50;

test.describe("alert inspector panel", () => {
  test.beforeEach(async ({ api }) => {
    await api.alerts.clear();
    await api.alerts.sendMany(
      Array.from({ length: PAGE_SIZE + 10 }, (_, i) => ({
        host: `srv-page${String(i).padStart(2, "0")}`,
        message: `event ${i}`,
        severity: "warning",
      })),
    );
    // Ingestion is asynchronous: without page 2 existing yet, the pager this
    // test is about renders disabled and the assertion below tests nothing.
    const deadline = Date.now() + 15_000;
    for (;;) {
      const rows = (await api.alerts.list()) as unknown[];
      if (rows.length > PAGE_SIZE) break;
      if (Date.now() >= deadline) throw new Error(`only ${rows.length} records ingested`);
      await new Promise((r) => setTimeout(r, 200));
    }
  });

  test("paging works with the inspector open, and the panel stays on its alert", async ({
    page,
    api,
    server,
  }) => {
    await loginAsAdmin(page, { baseURL: server.baseURL, token: api.token });
    await page.goto(`${server.baseURL}/web/alerts`);
    await expect(page.getByRole("button", { name: "Next page" })).toBeEnabled();

    await page
      .locator("tbody tr")
      .first()
      .getByRole("button", { name: "View details" })
      .click({ force: true });
    const panel = page.getByRole("dialog");
    await expect(panel).toBeVisible();

    // The click reaches the pager rather than a full-screen scrim…
    await page.getByRole("button", { name: "Next page" }).click();
    await expect(page).toHaveURL(/[?&]page=2/);
    // …and paging is not a reason to throw the operator's panel away.
    await expect(panel).toBeVisible();

    await page.getByRole("button", { name: "Previous page" }).click();
    await expect(page).toHaveURL(/[?&]page=1/);
    await expect(panel).toBeVisible();

    // Escape still closes it, and closing drops ?record= from the URL.
    await page.keyboard.press("Escape");
    await expect(panel).toBeHidden();
    await expect(page).not.toHaveURL(/[?&]record=/);
  });
});

// The same contract for the EDITOR panels (snoozes, rules, users, …): they are
// side panels too, so the list behind one stays live while a record is open.
test.describe("editor panel", () => {
  test("the list behind an open editor is still interactive", async ({ page, api, server }) => {
    await api.snoozes.create({
      name: "zz-second",
      condition: { type: "ALWAYS_TRUE" },
      comment: "e2e",
      time_constraints: {},
    });
    await api.snoozes.create({
      name: "aa-first",
      condition: { type: "ALWAYS_TRUE" },
      comment: "e2e",
      time_constraints: {},
    });
    await loginAsAdmin(page, { baseURL: server.baseURL, token: api.token });
    await page.goto(`${server.baseURL}/web/snoozes`);
    await expect(page.getByText("aa-first")).toBeVisible();

    await page.getByText("aa-first").click();
    const editor = page.getByRole("dialog");
    await expect(editor).toBeVisible();

    // A sort header behind the panel: unreachable under the old scrim.
    await page.getByRole("button", { name: /^Name/ }).first().click();
    await expect(page).toHaveURL(/orderby=name/);
    // The editor is not dismissed by working in the list — closing stays
    // deliberate, so the dirty-form guard can never be skipped by a stray click.
    await expect(editor).toBeVisible();
  });
});
