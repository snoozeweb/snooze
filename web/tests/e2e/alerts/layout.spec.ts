import { test, expect } from "../harness/fixtures";

// Regression coverage for the fixed-layout column budget (DataTable.module.css
// `.table { table-layout: fixed }` + alerts columns.tsx widths/tiers). Before
// the fix, width-less columns (Message, the quick-actions cell) split
// whatever space was left over under fixed layout — at container widths above
// every hide tier but below the sum of declared column widths, the leftover
// went negative and both columns collapsed to ~0px, hiding the message text
// and clipping the hover quick-action buttons. This sweeps a range of
// viewport widths and asserts the Message column stays a sane visible size
// and the table never triggers horizontal overflow on its scroll wrapper.
test.describe("alerts table layout", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.alerts.clear();
    await adminAuth();
  });

  test("message column stays visible and the table never overflows horizontally", async ({
    page,
    api,
    server,
  }) => {
    const longMessage =
      "disk almost full on /var/lib/postgresql — projected exhaustion in 36 hours, compaction recommended";
    await api.alerts.send({
      host: "srv-layout",
      message: longMessage,
      severity: "critical",
      source: "test",
    });

    const widths = [720, 900, 1200, 1500, 1750, 1920];
    for (const width of widths) {
      await page.setViewportSize({ width, height: 900 });
      await page.goto(server.baseURL + "/web/alerts");

      // At <=768px the container-query hide tier drops the Host column, so
      // anchor on the message text (always visible) rather than the host.
      const messageCell = page.getByText(longMessage, { exact: false }).first();
      await expect(messageCell).toBeVisible();
      const box = await messageCell.boundingBox();
      expect(box).not.toBeNull();
      // Floors from the width budget documented in columns.tsx (measured:
      // 220 / 250 / 330 / 542 / 672 / 746 across the widths below), kept
      // ~10-12% under so a font-metric or scrollbar difference isn't a
      // flake. Message now takes position 2 and the whole flexible
      // remainder, so it clears ~480px once the window is wide enough to
      // leave that much after the sidebar — where it used to get ~240px as
      // the last column. These floors were lowered because THREE separate
      // fixed-width columns grew, each taking its increase out of Message's
      // flexible remainder: 4768ae8d0 (Sev 100px -> 132px, so "Emergency" +
      // the trend arrow stop wrapping), 39b7a6073 (Hits 64px -> 88px, so a
      // 5-digit aggregate count stops being clipped) and af112e5cb (State
      // 112px -> 136px, for the longer "Acknowledged"/"Re-escalated"
      // nouns) — ~80px in total wherever all three are above their hide
      // tier. Each is a deliberate width-budget tradeoff, not a regression:
      // the 200-column, comfortably-clamped Message text at the narrowest
      // tier is still far from the near-0px collapse this test guards
      // against. The Owner column (64px, "lg" tier) took the 1500px floor
      // from 480 to 430 the same way: it shows from a 1024px container up,
      // so it costs Message exactly its width there (measured ~542 → ~478).
      const minWidth =
        width <= 720 ? 200 : width <= 900 ? 220 : width <= 1200 ? 290 : width <= 1500 ? 430 : 480;
      expect(box!.width).toBeGreaterThanOrEqual(minWidth);

      // No horizontal scroll: the table's own scroll wrapper (tableScroll)
      // is the parent of the role="grid" table — its content must never
      // exceed its own client width.
      const overflow = await page.getByRole("grid").evaluate((el) => {
        const p = el.parentElement!;
        return p.scrollWidth - p.clientWidth;
      });
      expect(overflow).toBeLessThanOrEqual(1);
    }
  });

  test("message leads the row and Hits marks only alerts that repeated", async ({
    page,
    api,
    server,
  }) => {
    await api.alerts.sendMany([
      { host: "srv-once", message: "happened once", severity: "info", source: "t" },
      { host: "srv-again", message: "happened a lot", severity: "warning", source: "t" },
    ]);
    // `duplicates` is stamped by the aggregate-rule pipeline on hash
    // collisions; PATCH it directly so one row is a repeat and one is not.
    const records = (await api.alerts.list()) as { uid?: string; host?: string }[];
    const repeat = records.find((r) => r.host === "srv-again");
    expect(repeat?.uid).toBeTruthy();
    await api.ctx.patch(`${server.baseURL}/api/v1/record/${repeat!.uid}`, {
      headers: { Authorization: `Bearer ${api.token}` },
      data: { duplicates: 14 },
    });

    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("happened once")).toBeVisible();

    // Message sits in position 2, right after severity — the header order is
    // the server's `defaultColumns`, so this also guards that list. The
    // leading blank header is the select-all checkbox cell.
    const headers = await page.getByRole("columnheader").allInnerTexts();
    // Header text is uppercased by CSS, which allInnerTexts() reflects.
    expect(headers.slice(0, 3).map((h) => h.trim())).toEqual(["", "SEV", "MESSAGE"]);
    // Process / Source are defined but demoted off the default layout.
    expect(headers.map((h) => h.trim())).not.toContain("PROCESS");
    expect(headers.map((h) => h.trim())).not.toContain("SOURCE");

    // A repeat shows its aggregation count; a singleton shows nothing at all
    // (this column used to be a stripe of em-dashes, which read as "no data").
    const hitsFor = (host: string) =>
      page.locator("tr", { hasText: host }).first().locator('td[data-label="Hits"]');
    await expect(hitsFor("srv-again")).toHaveText("×14");
    await expect(hitsFor("srv-once")).toHaveText("");
  });
});
