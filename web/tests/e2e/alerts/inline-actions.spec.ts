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

    // An undo toast surfaced, confirming the inline ack fired.
    await expect(page.getByText(/acknowledged srv-inline/i).first()).toBeVisible();

    // Acking moves the record out of the default "Alerts" tab (its preset
    // excludes state=ack), so the row leaves this view on refetch. Switch to
    // the "Acknowledged" tab and confirm the row is present with an "Ack"
    // state badge. Both checks are scoped to grid cells so the lifecycle tab
    // label and the still-visible undo-toast copy ("Acknowledged srv-inline")
    // don't satisfy the match. The Ack badge takes .first(): the 5s
    // auto-refresh poll can briefly render the re-fetched row alongside the
    // in-flight one, yielding two identical "Ack" badges mid-transition.
    await page.getByRole("tab", { name: /^acknowledged$/i }).click({ force: true });
    await expect(
      page.getByRole("gridcell").getByText("srv-inline", { exact: true }).first(),
    ).toBeVisible();
    await expect(
      page.getByRole("gridcell").getByText("Ack", { exact: true }).first(),
    ).toBeVisible();
  });

  test("the Undo toast re-opens the alert (compensating event)", async ({ page, api, server }) => {
    await api.alerts.send({ host: "srv-undo", message: "m", severity: "info", source: "t" });
    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("srv-undo")).toBeVisible();

    await page
      .getByRole("button", { name: /^acknowledge$/i })
      .first()
      .click({ force: true });

    // The ack pushes the row out of the default "Alerts" tab (preset excludes
    // state=ack); the undo toast is the in-view proof the ack landed. Wait for
    // it before clicking Undo within the 8s window — re-opens the alert.
    await expect(page.getByText(/acknowledged srv-undo/i).first()).toBeVisible();
    await page.getByRole("button", { name: /undo/i }).click({ force: true });

    // The compensating re-open returns the record to the Open state, so it
    // re-enters the default tab. The ack + re-open both persist on the backend
    // timeline; the table just reflects the latest state. Scope to a grid cell
    // (so the toast / aria-live copy don't match) and take .first(): the 5s
    // auto-refresh poll can briefly render the re-fetched row alongside the
    // in-flight one, yielding two identical "Open" badges mid-transition.
    await expect(
      page.getByRole("gridcell").getByText("srv-undo", { exact: true }).first(),
    ).toBeVisible();
    await expect(
      page.getByRole("gridcell").getByText("Open", { exact: true }).first(),
    ).toBeVisible();
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
