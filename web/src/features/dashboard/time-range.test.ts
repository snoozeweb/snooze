import { describe, expect, it } from "vitest";
import { isoToLocalInput, localInputToIso, presetToRange } from "./time-range";

describe("presetToRange", () => {
  it("returns a 24h window for '1d'", () => {
    const now = new Date("2026-05-14T12:00:00Z");
    const r = presetToRange("1d", now);
    expect(r.to).toBe("2026-05-14T12:00:00.000Z");
    expect(r.from).toBe("2026-05-13T12:00:00.000Z");
  });

  it("returns empty strings for 'custom'", () => {
    expect(presetToRange("custom").from).toBe("");
    expect(presetToRange("custom").to).toBe("");
  });

  it("returns ~30 days for '1m'", () => {
    const now = new Date("2026-05-14T00:00:00Z");
    const r = presetToRange("1m", now);
    expect(r.from).toBe("2026-04-14T00:00:00.000Z");
  });
});

describe("isoToLocalInput / localInputToIso", () => {
  it("round-trips an ISO instant through the local datetime-local shape", () => {
    const iso = "2026-05-14T12:30:00.000Z";
    const local = isoToLocalInput(iso);
    // Shape is the native datetime-local wire format (no seconds, no zone).
    expect(local).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/);
    // Re-encoding lands back on the same UTC instant (minute precision).
    expect(localInputToIso(local)).toBe("2026-05-14T12:30:00.000Z");
  });

  it("returns empty strings for empty / unparseable input", () => {
    expect(isoToLocalInput("")).toBe("");
    expect(isoToLocalInput("not-a-date")).toBe("");
    expect(localInputToIso("")).toBe("");
    expect(localInputToIso("garbage")).toBe("");
  });

  it("defaults missing time to 00:00 when parsing a date-only local string", () => {
    // The picker can emit a bare date before a time is chosen.
    const iso = localInputToIso("2026-05-14");
    expect(iso).not.toBe("");
    expect(isoToLocalInput(iso)).toBe("2026-05-14T00:00");
  });
});
