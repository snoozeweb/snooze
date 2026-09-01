// web/tests/e2e/dashboard/charts.spec.ts
//
// The dashboard's contract, against a real server: every number says where it
// comes from, the live half agrees with the alerts table, and suppressed
// events show up as removed noise. We don't pixel-match the charts.
//
// Counter documents are cumulative and survive `alerts.clear()` (they are
// event counters, not rows), and the whole file shares one server with every
// other spec in the worker — so the windowed assertions here are written as
// deltas, never as absolute totals.
import { test, expect } from "../harness/fixtures";
import type { Page } from "@playwright/test";

/** Numeric value shown on a KPI tile, by tile id. */
async function tileValue(page: Page, id: string): Promise<number> {
  const text = await page.locator(`[data-tile="${id}"] b`).innerText();
  return Number(text.replace(/\D/g, ""));
}

test.describe("dashboard", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.alerts.clear();
    await adminAuth();
  });

  test("renders the noise-removed panel, the time series and the supporting panels", async ({
    page,
    server,
  }) => {
    await page.goto(server.baseURL + "/web/dashboard");
    await expect(page.getByRole("heading", { name: /^dashboard$/i })).toBeVisible();
    await expect(page.getByRole("heading", { name: /noise removed/i })).toBeVisible();
    await expect(page.getByRole("heading", { name: /alerts over time/i })).toBeVisible();
    await expect(page.getByRole("heading", { name: /^by state$/i })).toBeVisible();
    await expect(page.getByRole("heading", { name: /^breakdowns$/i })).toBeVisible();
    await expect(page.getByRole("tab", { name: "Severity" })).toBeVisible();
    await expect(page.getByRole("tab", { name: "Actions" })).toBeVisible();

    // Each cluster names its source, so a live count and a windowed count can
    // never be read as the same kind of number.
    await expect(page.getByRole("region", { name: "Right now" })).toBeVisible();
    await expect(page.getByRole("region", { name: "Last 24 hours" })).toBeVisible();
  });

  test("an empty window says so, and says how to see more", async ({ page, api, server }) => {
    // Make sure this instance has counters at all — otherwise the honest
    // answer is the *other* empty state ("no counters yet"), which is what a
    // truly fresh install gets.
    await api.alerts.send({ host: "srv-a", message: "hello", severity: "info", source: "e2e" });

    // A window that ended long before this server existed: counters exist,
    // none of them in here. The copy must name that case rather than printing
    // a bare "No data."
    const to = Date.UTC(2020, 0, 2);
    const from = Date.UTC(2020, 0, 1);
    await page.goto(`${server.baseURL}/web/dashboard?range=custom&from=${from}&to=${to}`);
    await expect(page.getByText(/no events in this window/i).first()).toBeVisible();
    await expect(page.getByText(/try a wider range/i).first()).toBeVisible();
  });

  test("ingested events reach the windowed counters and agree with the live count", async ({
    page,
    api,
    server,
  }) => {
    await page.goto(server.baseURL + "/web/dashboard");
    const before = await tileValue(page, "ingested");

    await api.alerts.sendMany([
      { host: "srv-a", message: "disk full", severity: "critical", source: "e2e" },
      { host: "srv-b", message: "cpu high", severity: "warning", source: "e2e" },
      { host: "srv-c", message: "link flap", severity: "err", source: "e2e" },
    ]);
    await page.reload();

    // Live cluster: three alerts in the queue, exactly what the alerts table
    // lists (same ACTIVE_ALERTS query as the sidebar badge).
    await expect(page.locator('[data-tile="attention"] b')).toHaveText("3");

    // Windowed cluster: the counter pipeline recorded the same three events.
    // Regression guard for counters bucketed at epoch 0, which showed
    // "Ingested 0" next to a non-zero live count on every real deployment.
    await expect
      .poll(async () => (await tileValue(page, "ingested")) - before)
      .toBeGreaterThanOrEqual(3);
  });

  test("a snooze filter shows up as removed noise", async ({ page, api, server }) => {
    await api.snoozes.create({
      name: "e2e-quiet-host",
      condition: { type: "=", field: "host", value: "srv-noisy" },
      enabled: true,
      discard: true,
    });

    // The snooze plugin serves from a reloaded cache, so poll until a matching
    // event is actually being dropped before measuring anything.
    await expect
      .poll(
        async () => {
          // Clear first: a row written by an earlier probe (before the cache
          // reloaded) would otherwise keep the predicate true forever.
          await api.alerts.clear();
          await api.alerts.send({
            host: "srv-noisy",
            message: "chatter",
            severity: "warning",
            source: "e2e",
          });
          const rows = (await api.alerts.list()) as { host?: string }[];
          return rows.some((r) => r.host === "srv-noisy");
        },
        { timeout: 20_000 },
      )
      .toBe(false);

    await page.goto(server.baseURL + "/web/dashboard");
    const snoozedBefore = await tileValue(page, "snoozed");

    await api.alerts.sendMany([
      { host: "srv-noisy", message: "chatter", severity: "warning", source: "e2e" },
      { host: "srv-noisy", message: "chatter again", severity: "warning", source: "e2e" },
      { host: "srv-quiet", message: "real problem", severity: "critical", source: "e2e" },
    ]);
    await page.reload();

    await expect
      .poll(async () => (await tileValue(page, "snoozed")) - snoozedBefore)
      .toBeGreaterThanOrEqual(2);

    // And the thesis panel accounts for them in the ingest split.
    const noise = page.getByRole("heading", { name: /noise removed/i }).locator("..");
    await expect(noise.getByText(/events suppressed/i)).toBeVisible();
    await expect(noise.getByText("Snoozed", { exact: true })).toBeVisible();

    await api.snoozes.clear();
  });
});
