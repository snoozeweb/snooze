import { test, expect } from "../harness/fixtures";

// Phase 5 — one-click inline quick actions + undo.
//
// The alerts table now reveals inline IconButtons (ack / close / comment)
// before the kebab. Inline ack/close skip the confirm dialog the kebab path
// uses and fire the comment mutation directly, then raise an "Undo" toast.
// The undo is a compensating event: it POSTs type:"open", so the timeline
// keeps both the ack and the re-open — we assert the state round-trips
// Open → Ack → Open.
test.describe("alerts inline quick actions", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.alerts.clear();
    await adminAuth();
  });

  test("inline ack flips the row to Ack without opening a dialog", async ({
    page,
    api,
    server,
  }) => {
    await api.alerts.send({ host: "srv-inline", message: "m", severity: "info", source: "t" });
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("srv-inline")).toBeVisible();

    // The inline "Acknowledge" IconButton (aria-label "Acknowledge"), NOT the
    // kebab menu item. Clicking it must NOT pop the confirm dialog.
    await page
      .getByRole("button", { name: /^acknowledge$/i })
      .first()
      .click({ force: true });
    await expect(page.getByRole("dialog")).toHaveCount(0);

    // State badge moves to "Ack".
    await expect(page.getByText("Ack", { exact: true })).toBeVisible();

    // An undo toast surfaced.
    await expect(page.getByText(/acknowledged srv-inline/i).first()).toBeVisible();
  });

  test("the Undo toast re-opens the alert (compensating event)", async ({ page, api, server }) => {
    await api.alerts.send({ host: "srv-undo", message: "m", severity: "info", source: "t" });
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("srv-undo")).toBeVisible();

    await page
      .getByRole("button", { name: /^acknowledge$/i })
      .first()
      .click({ force: true });
    await expect(page.getByText("Ack", { exact: true })).toBeVisible();

    // Click Undo within the 8s window — re-opens the alert.
    await page.getByRole("button", { name: /undo/i }).click({ force: true });

    // The record returns to the Open state (the ack + re-open both persist on
    // the backend timeline; the table just reflects the latest state).
    await expect(page.getByText("Open", { exact: true })).toBeVisible();
  });

  test("inline close moves the alert to Closed", async ({ page, api, server }) => {
    await api.alerts.send({ host: "srv-iclose", message: "m", severity: "info", source: "t" });
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("srv-iclose")).toBeVisible();

    await page
      .getByRole("button", { name: /^close$/i })
      .first()
      .click({ force: true });
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByText("Closed", { exact: true })).toBeVisible();
  });
});
