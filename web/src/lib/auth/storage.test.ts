import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  clearToken,
  readClaims,
  readRefreshToken,
  readToken,
  writeRefreshToken,
  writeToken,
} from "./storage";

function makeToken(payload: object): string {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(JSON.stringify(payload));
  return `${header}.${body}.sig`;
}

describe("auth storage", () => {
  beforeEach(() => {
    localStorage.clear();
  });
  afterEach(() => {
    localStorage.clear();
  });

  it("readToken returns null when nothing is stored", () => {
    expect(readToken()).toBeNull();
  });

  it("writeToken / readToken round-trips", () => {
    writeToken("abc.def.ghi");
    expect(readToken()).toBe("abc.def.ghi");
  });

  it("writeToken caches decoded claims under snooze-claims", () => {
    const tok = makeToken({ sub: "alice", exp: 9999999999 });
    writeToken(tok);
    const claims = readClaims();
    expect(claims?.sub).toBe("alice");
    expect(claims?.exp).toBe(9999999999);
  });

  it("clearToken removes the access token, claims and refresh token", () => {
    writeToken(makeToken({ sub: "x", exp: 9999999999 }));
    writeRefreshToken("opaque-refresh");
    clearToken();
    expect(readToken()).toBeNull();
    expect(readClaims()).toBeNull();
    expect(readRefreshToken()).toBeNull();
  });

  it("readClaims returns null on a malformed cache", () => {
    localStorage.setItem("snooze-claims", "{not json");
    expect(readClaims()).toBeNull();
  });

  it("writeRefreshToken / readRefreshToken round-trips", () => {
    writeRefreshToken("opaque-refresh-token-xyz");
    expect(readRefreshToken()).toBe("opaque-refresh-token-xyz");
  });

  it("writeRefreshToken(null) removes the stored token", () => {
    writeRefreshToken("opaque");
    writeRefreshToken(null);
    expect(readRefreshToken()).toBeNull();
  });
});
