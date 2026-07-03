// web/tests/e2e/snoozes/timezone.spec.ts
//
// #31 — snooze datetime activation timezone, proven live against the real Go
// backend with the browser pinned to Europe/Paris.
//
// The bug: the calendar date-picker used to emit a zone-less "YYYY-MM-DDTHH:MM"
// value, which the backend parsed as UTC — so a window an operator set for
// 15:00 Paris actually fired at 15:00 UTC (17:00 Paris in summer). The fix
// makes the picker emit ZONED RFC3339 with the offset computed for the picked
// date, so the stored instant is the one the operator meant, and it is DST-safe
// (summer +02:00, winter +01:00) because JS resolves the offset per-date.
//
// The shared `page` fixture builds its BrowserContext without a timezoneId, so
// these tests mint their own Paris-pinned context off the worker's CDP browser.
import { test, expect } from "../harness/fixtures";
import { loginAsAdmin } from "../harness/auth";
import type { Browser, BrowserContext, Page } from "@playwright/test";
import type { ServerHandle } from "../harness/server";
import type { SnoozeApi } from "../harness/api";

const PARIS = "Europe/Paris";

// disableAnimations mirrors the shared `page` fixture: Radix's drawer/popover
// slide-ins otherwise leave elements mid-flight when Playwright measures them.
async function disableAnimations(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const style = document.createElement("style");
    style.textContent =
      "*, *::before, *::after { animation: none !important; transition: none !important; }";
    const insert = () => document.head?.appendChild(style);
    if (document.head) insert();
    else document.addEventListener("DOMContentLoaded", insert);
  });
}

// parisContext opens a Paris-pinned, logged-in page and lands on the snoozes
// list. Returns the context so the caller can close it.
async function parisContext(
  cdpBrowser: Browser,
  server: ServerHandle,
  api: SnoozeApi,
): Promise<{ ctx: BrowserContext; page: Page }> {
  const ctx = await cdpBrowser.newContext({ timezoneId: PARIS });
  const page = await ctx.newPage();
  await disableAnimations(page);
  await loginAsAdmin(page, { baseURL: server.baseURL, token: api.token });
  // Fail loudly (rather than silently on the OS zone) if the harness cannot
  // emulate the timezone over CDP — the whole point of these tests is the zone.
  const zone = await page.evaluate(() => Intl.DateTimeFormat().resolvedOptions().timeZone);
  expect(zone, "browser must be pinned to Europe/Paris for the DST assertions").toBe(PARIS);
  return { ctx, page };
}

// navigateToMonthAndPick15 walks the react-day-picker calendar forward until
// the 15th of `monthName` is on screen, then clicks it. Day 15 is always an
// in-month day (never one of the greyed adjacent-month "outside" days), so its
// button is unambiguous, and forward navigation reaches the next occurrence of
// any month within a year regardless of what "today" is.
async function navigateToMonthAndPick15(page: Page, monthName: string): Promise<void> {
  const day = page.getByRole("button", { name: new RegExp(`${monthName} 15th`) }).first();
  const next = page.getByRole("button", { name: "Go to the Next Month" });
  for (let i = 0; i < 14; i++) {
    if (await day.isVisible().catch(() => false)) break;
    await next.click({ force: true });
  }
  await expect(day).toBeVisible();
  await day.click({ force: true });
}

// createSnoozeWithPickedRange drives the full New-snooze flow: name it, add an
// absolute date range, open the picker, pick the 15th of `monthName`, set the
// from-time, save. Returns after the success toast.
async function createSnoozeWithPickedRange(
  page: Page,
  server: ServerHandle,
  opts: { name: string; monthName: string; fromTime: string },
): Promise<void> {
  await page.goto(server.baseURL + "/web/snoozes");
  await page.getByRole("button", { name: /^new$/i }).click({ force: true });
  await expect(page.getByRole("heading", { name: /new snooze/i })).toBeVisible();

  await page.getByLabel("Name").fill(opts.name);

  // "Add range" seeds an empty absolute date range and renders its picker.
  await page.getByRole("button", { name: /^add range$/i }).click({ force: true });
  await page
    .getByRole("button", { name: /Date range 1 from \/ Date range 1 until/ })
    .click({ force: true });

  await navigateToMonthAndPick15(page, opts.monthName);

  // The time input shares the "Date range 1 from" label with the trigger, so
  // match it exactly (the trigger's accessible name is longer).
  await page.getByLabel("Date range 1 from", { exact: true }).fill(opts.fromTime);

  await page.keyboard.press("Escape"); // close the popover
  await page.getByRole("button", { name: /^create$/i }).click({ force: true });
  await expect(page.getByText(/snooze created/i).first()).toBeVisible();
}

// fromBound reads back the stored `time_constraints.datetime[0].from` for the
// named snooze via the API — i.e. exactly the wire value the backend persisted.
async function fromBound(api: SnoozeApi, name: string): Promise<string> {
  const list = (await api.snoozes.list()) as Array<{
    name?: string;
    time_constraints?: { datetime?: Array<{ from?: string }> };
  }>;
  const found = list.find((s) => s.name === name);
  expect(found, `snooze ${name} should exist`).toBeTruthy();
  const from = found?.time_constraints?.datetime?.[0]?.from;
  expect(from, "stored datetime range should carry a from bound").toBeTruthy();
  return from as string;
}

test.describe("snooze datetime timezone (#31)", () => {
  test.beforeEach(async ({ api }) => {
    await api.snoozes.clear();
  });

  test("the pinned browser resolves Paris DST offsets per-date", async ({
    cdpBrowser,
    server,
    api,
  }) => {
    // Guards the primitive composeZonedIso relies on: getTimezoneOffset()
    // returns minutes BEHIND UTC, so summer (+02:00) is -120 and winter
    // (+01:00) is -60 — in the real, zone-pinned browser.
    const { ctx, page } = await parisContext(cdpBrowser, server, api);
    try {
      const offsets = await page.evaluate(() => ({
        july: new Date(2026, 6, 15, 12, 0).getTimezoneOffset(),
        january: new Date(2026, 0, 15, 12, 0).getTimezoneOffset(),
      }));
      expect(offsets.july).toBe(-120);
      expect(offsets.january).toBe(-60);
    } finally {
      await ctx.close();
    }
  });

  test("a summer date range is stored with a +02:00 offset (not UTC)", async ({
    cdpBrowser,
    server,
    api,
  }) => {
    const { ctx, page } = await parisContext(cdpBrowser, server, api);
    try {
      await createSnoozeWithPickedRange(page, server, {
        name: "e2e-tz-summer",
        monthName: "July",
        fromTime: "15:00",
      });
      const from = await fromBound(api, "e2e-tz-summer");
      // The operator meant 15:00 Paris on a July day → 13:00 UTC. The stored
      // value must carry the summer offset, and must NOT be a bare/UTC value.
      expect(from).toMatch(/T15:00:00\+02:00$/);
      expect(new Date(from).getUTCHours()).toBe(13);
    } finally {
      await ctx.close();
    }
  });

  test("a winter date range is stored with a +01:00 offset (DST-safe)", async ({
    cdpBrowser,
    server,
    api,
  }) => {
    const { ctx, page } = await parisContext(cdpBrowser, server, api);
    try {
      await createSnoozeWithPickedRange(page, server, {
        name: "e2e-tz-winter",
        monthName: "January",
        fromTime: "09:00",
      });
      const from = await fromBound(api, "e2e-tz-winter");
      // Same zone, a winter day → +01:00, so 09:00 Paris is 08:00 UTC. A fixed
      // offset baked from "today" would get this wrong half the year; the
      // per-date offset does not.
      expect(from).toMatch(/T09:00:00\+01:00$/);
      expect(new Date(from).getUTCHours()).toBe(8);
    } finally {
      await ctx.close();
    }
  });
});
