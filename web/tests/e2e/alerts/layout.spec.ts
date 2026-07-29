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
      const minWidth = width <= 720 ? 60 : 150;
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
});
