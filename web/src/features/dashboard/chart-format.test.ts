import { describe, expect, it } from "vitest";
import { capDistributions, formatBucketLabel } from "./chart-format";

// TZ-tolerant: assert the shape of the label per range, not exact clock values
// (which depend on the runner's timezone).
describe("formatBucketLabel", () => {
  const iso = "2026-06-25T14:30:00Z";

  it("shows time of day for a 1-day range", () => {
    expect(formatBucketLabel(iso, "1d")).toMatch(/^\d{2}:\d{2}$/);
  });

  it("shows weekday + time for a 1-week range", () => {
    expect(formatBucketLabel(iso, "1w")).toMatch(/^(Sun|Mon|Tue|Wed|Thu|Fri|Sat) \d{2}:\d{2}$/);
  });

  it("shows a bare date for the year range (daily buckets)", () => {
    expect(formatBucketLabel(iso, "1y")).toMatch(/^[A-Z][a-z]{2} \d{1,2}$/);
  });

  it("keeps time on the month range so its 6h buckets stay distinguishable", () => {
    // date-only would collapse four 6h buckets to one label + ambiguous tooltip.
    expect(formatBucketLabel(iso, "1m")).toMatch(/^[A-Z][a-z]{2} \d{1,2}, \d{2}:\d{2}$/);
  });

  it("shows date + time for a custom range", () => {
    expect(formatBucketLabel(iso, "custom")).toMatch(/^[A-Z][a-z]{2} \d{1,2}, \d{2}:\d{2}$/);
  });

  it("never renders a worse label than the raw string on an invalid date", () => {
    expect(formatBucketLabel("not-a-date", "1d")).toBe("not-a-date");
  });
});

describe("capDistributions", () => {
  it("keeps the top-N keys ranked by value in a single series", () => {
    const { keys, capped } = capDistributions([{ x: 10, y: 5, z: 1 }], 2);
    expect(keys).toEqual(["x", "y"]);
    expect(capped[0]).toEqual({ x: 10, y: 5 });
  });

  it("ranks by the combined total across multiple series", () => {
    const success = { deploy: 1, page: 8 };
    const failure = { deploy: 9, page: 0 };
    const { keys, capped } = capDistributions([success, failure], 1);
    // deploy total 10 > page total 8 → deploy wins, and both series are capped
    // to the same surviving key set.
    expect(keys).toEqual(["deploy"]);
    expect(capped[0]).toEqual({ deploy: 1 });
    expect(capped[1]).toEqual({ deploy: 9 });
  });

  it("returns everything when there are fewer keys than the cap", () => {
    const { keys } = capDistributions([{ a: 1, b: 2 }], 10);
    expect(keys.sort()).toEqual(["a", "b"]);
  });

  it("reports the pre-cap distinct-key total so callers can label truncation", () => {
    const { total, keys } = capDistributions([{ a: 5, b: 4, c: 3, d: 2, e: 1 }], 2);
    expect(total).toBe(5);
    expect(keys).toEqual(["a", "b"]);
  });
});
