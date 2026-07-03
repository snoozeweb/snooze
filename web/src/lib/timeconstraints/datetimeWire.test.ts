import { describe, expect, it } from "vitest";
import { composeZonedIso, formatOffsetIso, splitZonedIso } from "./datetimeWire";

const pad = (n: number) => String(n).padStart(2, "0");

// These tests are zone-independent by design: they assert the round-trip
// PROPERTY (the emitted string denotes the intended local instant, and reads
// back to the same wall clock) rather than a hard-coded offset. The actual
// summer-vs-winter offset divergence — the DST-safety guarantee — is proven
// end-to-end against the real backend in tests/e2e/snoozes/timezone.spec.ts,
// which pins the browser to Europe/Paris.

describe("composeZonedIso", () => {
  it("emits zoned RFC3339 with seconds and an offset", () => {
    const s = composeZonedIso(new Date(2026, 4, 15), "10:30");
    expect(s).toMatch(/^2026-05-15T10:30:00[+-]\d{2}:\d{2}$/);
  });

  it("denotes exactly the intended LOCAL instant (offset matches the picked date)", () => {
    // Whatever zone the runner is in, parsing the emitted string back must
    // yield the same absolute instant as the local wall clock the user picked.
    const local = new Date(2026, 0, 15, 9, 0, 0, 0); // Jan 15 09:00 local
    const s = composeZonedIso(local, "09:00");
    expect(new Date(s as string).getTime()).toBe(local.getTime());
  });

  it("defaults a missing/garbled time to 00:00", () => {
    expect(composeZonedIso(new Date(2026, 4, 15), "")).toMatch(/T00:00:00/);
    expect(composeZonedIso(new Date(2026, 4, 15), "nonsense")).toMatch(/T00:00:00/);
  });

  it("returns undefined when there is no date", () => {
    expect(composeZonedIso(null, "10:30")).toBeUndefined();
    expect(composeZonedIso(undefined, "10:30")).toBeUndefined();
  });
});

describe("splitZonedIso", () => {
  it("renders a zoned value in the viewer's local zone", () => {
    const wire = "2026-01-15T09:00:00+01:00";
    const asDate = new Date(wire);
    const { date, time } = splitZonedIso(wire);
    expect(time).toBe(`${pad(asDate.getHours())}:${pad(asDate.getMinutes())}`);
    expect(date?.getFullYear()).toBe(asDate.getFullYear());
    expect(date?.getMonth()).toBe(asDate.getMonth());
    expect(date?.getDate()).toBe(asDate.getDate());
  });

  it("also handles a trailing-Z (UTC) value via the zoned path", () => {
    const asDate = new Date("2026-06-01T12:00:00Z");
    const { time } = splitZonedIso("2026-06-01T12:00:00Z");
    expect(time).toBe(`${pad(asDate.getHours())}:${pad(asDate.getMinutes())}`);
  });

  it("keeps a legacy zone-less value as its literal wall clock (no UTC drift)", () => {
    const { date, time } = splitZonedIso("2026-07-02T09:00");
    expect(date?.getFullYear()).toBe(2026);
    expect(date?.getMonth()).toBe(6); // July (0-indexed)
    expect(date?.getDate()).toBe(2);
    expect(time).toBe("09:00");
  });

  it("handles a date-only value (no time component)", () => {
    const { date, time } = splitZonedIso("2026-07-02");
    expect(date?.getDate()).toBe(2);
    expect(time).toBe("");
  });

  it("returns an empty split for undefined / unparseable input", () => {
    expect(splitZonedIso(undefined)).toEqual({ date: null, time: "" });
    expect(splitZonedIso("not-a-date")).toEqual({ date: null, time: "" });
  });
});

describe("round-trip", () => {
  it("compose → split preserves the calendar day and clock", () => {
    const picked = new Date(2026, 10, 3); // Nov 3 2026
    const wire = composeZonedIso(picked, "16:45");
    const { date, time } = splitZonedIso(wire);
    expect(time).toBe("16:45");
    expect(date?.getFullYear()).toBe(2026);
    expect(date?.getMonth()).toBe(10);
    expect(date?.getDate()).toBe(3);
  });
});

describe("formatOffsetIso", () => {
  it("round-trips a Date through an RFC3339 string to the same instant", () => {
    const d = new Date(2026, 2, 29, 2, 30, 0, 0); // a spring-forward-ish local time
    expect(new Date(formatOffsetIso(d)).getTime()).toBe(d.getTime());
  });
});
