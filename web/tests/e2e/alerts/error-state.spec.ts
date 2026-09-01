import { test, expect } from "../harness/fixtures";

// Regression guard for the worst copy this product can emit: with the record
// endpoint down, the alerts table used to render the green "All clear — every
// alert has been triaged" card, because "no rows" and "we never got an answer"
// took the same code path. The only failure signal was a small badge next to
// the auto-refresh switch.
test.describe("alerts list error state", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.alerts.clear();
    await adminAuth();
  });

  test("a failing record query shows the error panel, never 'All clear', and Try again recovers", async ({
    page,
    api,
    server,
  }) => {
    await api.alerts.send({
      host: "srv-errorstate",
      message: "still firing",
      severity: "critical",
      source: "test",
    });

    // Fail only the list read; every other API call (config, stats, session)
    // still works, which is exactly the shape of a storage-backend outage.
    let failing = true;
    // Matched by pathname (not a glob) so `/record/bulk_state` and friends
    // stay untouched.
    await page.route(
      (url) => url.pathname === "/api/v1/record",
      async (route) => {
        if (!failing) return route.fallback();
        await route.fulfill({
          status: 500,
          contentType: "application/json",
          body: JSON.stringify({ error: { code: "internal", message: "storage unavailable" } }),
        });
      },
    );

    await page.goto(server.baseURL + "/web/alerts");

    await expect(page.getByText(/can't reach the alert store/i)).toBeVisible();
    // The dangerous sentence must be nowhere on the page.
    await expect(page.getByText(/all clear/i)).toHaveCount(0);
    await expect(page.getByText(/every alert has been triaged/i)).toHaveCount(0);
    // The panel states the staleness and carries the server's own words.
    await expect(page.getByText(/no data loaded/i)).toBeVisible();
    await expect(page.getByText(/storage unavailable/i).first()).toBeVisible();
    // …and the toolbar says it too, loudly.
    await expect(page.getByRole("button", { name: /not updating/i })).toBeVisible();

    // Stop the 5s poll so the recovery below is provably the retry's doing.
    await page.getByRole("switch", { name: /auto refresh/i }).click({ force: true });
    failing = false;
    await expect(page.getByText(/can't reach the alert store/i)).toBeVisible();

    await page.getByRole("button", { name: /try again/i }).click({ force: true });
    await expect(page.getByText("srv-errorstate")).toBeVisible();
    await expect(page.getByText(/can't reach the alert store/i)).toHaveCount(0);
  });
});
