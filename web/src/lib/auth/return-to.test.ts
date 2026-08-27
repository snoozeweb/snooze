import { describe, expect, it } from "vitest";
import { isSafeInternalPath, loginRedirectSearch, normalizeReturnTo } from "./return-to";

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

describe("loginRedirectSearch", () => {
  it("carries the current path unencoded (the router encodes on the way out)", () => {
    // Pre-encoding here is the bug this replaced: the router decodes search
    // values once on read, so "%2Fweb%2Falerts" would reach the login page
    // still encoded, fail isSafeInternalPath, and silently lose the return.
    expect(loginRedirectSearch("/web/alerts?q=host%3Dweb1")).toEqual({
      return_to: "/web/alerts?q=host%3Dweb1",
    });
  });

  it("reduces an absolute same-origin URL to its path + query", () => {
    expect(loginRedirectSearch("http://localhost:5173/web/rules?page=2")).toEqual({
      return_to: "/web/rules?page=2",
    });
  });

  it("never captures the login page itself", () => {
    expect(loginRedirectSearch("/web/login")).toEqual({});
    expect(loginRedirectSearch("/web/login?sso_error=nope")).toEqual({});
  });

  it("drops unsafe / empty destinations", () => {
    expect(loginRedirectSearch("//evil.example")).toEqual({});
    expect(loginRedirectSearch("")).toEqual({});
    expect(loginRedirectSearch(null)).toEqual({});
  });
});

describe("normalizeReturnTo", () => {
  it("passes a decoded path through", () => {
    expect(normalizeReturnTo("/web/alerts?q=a%3Db")).toBe("/web/alerts?q=a%3Db");
  });

  it("accepts a still-encoded value minted by an older build", () => {
    expect(normalizeReturnTo("%2Fweb%2Falerts")).toBe("/web/alerts");
  });

  it("rejects unsafe destinations in both forms", () => {
    expect(normalizeReturnTo("//evil.example")).toBeNull();
    expect(normalizeReturnTo(encodeURIComponent("//evil.example"))).toBeNull();
    expect(normalizeReturnTo("/web/x?p=100%")).toBe("/web/x?p=100%");
    expect(normalizeReturnTo(undefined)).toBeNull();
  });
});
