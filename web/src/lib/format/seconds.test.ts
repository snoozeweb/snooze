import { describe, expect, it } from "vitest";
import { secondsToHuman } from "./seconds";

describe("secondsToHuman", () => {
  it("renders 0 as 'forever' (matches legacy snooze TTL semantics)", () => {
    expect(secondsToHuman(0)).toBe("forever");
  });
  it("renders a dash for undefined / negative", () => {
    expect(secondsToHuman(undefined)).toBe("—");
    expect(secondsToHuman(null)).toBe("—");
    expect(secondsToHuman(-1)).toBe("—");
  });
  it("renders sub-minute values in seconds only", () => {
    expect(secondsToHuman(30)).toBe("30s");
    expect(secondsToHuman(1)).toBe("1s");
  });
  it("renders minutes", () => {
    expect(secondsToHuman(60)).toBe("1m");
    expect(secondsToHuman(125)).toBe("2m 5s");
  });
  it("renders hours and days", () => {
    expect(secondsToHuman(3600)).toBe("1h");
    expect(secondsToHuman(86400)).toBe("1d");
    expect(secondsToHuman(86400 + 3600)).toBe("1d 1h");
  });
  it("caps output at two significant units", () => {
    expect(secondsToHuman(2 * 86400 + 4 * 3600 + 30 * 60 + 5)).toBe("2d 4h");
  });
});
