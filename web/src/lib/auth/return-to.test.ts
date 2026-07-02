import { describe, expect, it } from "vitest";
import { isSafeInternalPath } from "./return-to";

describe("isSafeInternalPath", () => {
  it("accepts a normal same-origin path (with encoded query state intact)", () => {
    expect(isSafeInternalPath("/web/alerts")).toBe(true);
    expect(isSafeInternalPath("/web/alerts?q=a%3Db")).toBe(true);
    expect(isSafeInternalPath("/web/x?p=100%")).toBe(true);
  });

  it("rejects protocol-relative and cross-origin destinations (open-redirect)", () => {
    expect(isSafeInternalPath("//evil.example")).toBe(false);
    expect(isSafeInternalPath("/\\evil.example")).toBe(false);
    expect(isSafeInternalPath("\\/evil.example")).toBe(false);
    expect(isSafeInternalPath("http://evil.example")).toBe(false);
    expect(isSafeInternalPath("javascript:alert(1)")).toBe(false);
  });

  it("rejects empty / missing values", () => {
    expect(isSafeInternalPath("")).toBe(false);
    expect(isSafeInternalPath(undefined)).toBe(false);
    expect(isSafeInternalPath(null)).toBe(false);
  });
});
