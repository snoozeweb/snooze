import { test, expect } from "../harness/fixtures";

// Verifies the "commit search to the URL" behaviour end-to-end: pressing Enter
// on a clean query writes ?search=, the filter applies, it survives a reload,
// and clearing drops the param. Covers both the Alerts page (custom search
// wiring) and the Rules page (the shared useTableSearch path used by every
// other list page), so the global wiring is exercised.
test.describe("search query syncs to the URL", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.alerts.clear();
    await adminAuth();
  });

  test("Alerts: Enter commits ?search=, persists on reload, clear drops it", async ({
    page,
    api,
    server,
  }) => {
    await api.alerts.sendMany([
      { host: "h1", message: "a", severity: "info", source: "t" },
      { host: "h2", message: "b", severity: "critical", source: "t" },
    ]);
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("h1")).toBeVisible();
    await expect(page.getByText("h2")).toBeVisible();

    const search = page.getByRole("textbox", { name: /^search$/i });
    await search.fill("severity = critical");
    // Close the autocomplete popover so Enter commits the query rather than
    // accepting a highlighted suggestion (the real-UX two-step), then commit.
    await search.press("Escape");
    await search.press("Enter");

    // The committed query is now in the address bar…
    await expect(page).toHaveURL(/[?&]search=severity/);
    // …and the server-side filter is applied (only the critical row survives).
    await expect(page.getByText("h2")).toBeVisible();
    await expect(page.getByText("h1")).toBeHidden();

    // Survives a hard reload: the field re-seeds from the URL and the filter
    // is still applied.
    await page.reload();
    await expect(page.getByRole("textbox", { name: /^search$/i })).toHaveValue(
      "severity = critical",
    );
    await expect(page.getByText("h2")).toBeVisible();
    await expect(page.getByText("h1")).toBeHidden();

    // Clearing the field drops the param from the URL.
    await page.getByRole("button", { name: /clear search/i }).click();
    await expect(page).not.toHaveURL(/[?&]search=/);
  });

  test("Rules (shared useTableSearch): Enter commits ?search= and survives reload", async ({
    page,
    server,
  }) => {
    await page.goto(server.baseURL + "/web/rules");
    const search = page.getByRole("textbox", { name: /^search$/i });
    await expect(search).toBeVisible();

    await search.fill("name = probe");
    await search.press("Escape");
    await search.press("Enter");

    await expect(page).toHaveURL(/[?&]search=name/);

    await page.reload();
    await expect(page.getByRole("textbox", { name: /^search$/i })).toHaveValue("name = probe");
  });
});
