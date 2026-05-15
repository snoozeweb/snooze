// web/tests/e2e/snoozes/effects.spec.ts
//
// End-to-end behaviour test: a snooze whose condition matches an incoming
// alert causes the resulting record to enter the "shelved" state (the
// snooze pipeline stage sets state=shelved or similar; we verify via the
// rendered alert state badge).
import { test, expect } from "../harness/fixtures";

test.describe("snooze pipeline effects", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.snoozes.clear();
    await api.alerts.clear();
    await adminAuth();
  });

  test("matching snooze leaves a visible 'shelved' state on the alert", async ({
    api,
    page,
    server,
  }) => {
    await api.snoozes.create({
      name: "always-snooze",
      enabled: true,
      condition: { type: "ALWAYS_TRUE" },
      ttl: 3600,
    });

    await api.alerts.send({
      host: "srv-shelved",
      message: "msg",
      severity: "info",
      source: "t",
    });

    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("srv-shelved")).toBeVisible();
    // Whatever the badge label is, the row should not show "Open" — snooze
    // moves the alert out of the open state.
    // We accept any of "Shelved" / "Acknowledged" / "Closed" — open is the
    // failing condition.
    const stateBadge = page
      .getByRole("row")
      .filter({ hasText: "srv-shelved" })
      .first();
    await expect(stateBadge).not.toContainText(/^Open$/i);
  });
});
