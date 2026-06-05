import { describe, expect, it } from "vitest";
import { decodeJwt, isExpired, secondsUntilExpiry, type JwtClaims } from "./jwt";

function makeToken(payload: object): string {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(JSON.stringify(payload));
  return `${header}.${body}.signature`;
}

describe("decodeJwt", () => {
  it("decodes a well-formed token", () => {
    const tok = makeToken({ sub: "alice", exp: 9999999999 });
    const claims = decodeJwt(tok);
    expect(claims).not.toBeNull();
    expect(claims!.sub).toBe("alice");
    expect(claims!.exp).toBe(9999999999);
  });

  it("returns null on garbage input", () => {
    expect(decodeJwt("not.a.jwt")).toBeNull();
    expect(decodeJwt("")).toBeNull();
    expect(decodeJwt("only.two")).toBeNull();
  });
});

describe("isExpired", () => {
  it("returns true when exp is in the past", () => {
    const claims: JwtClaims = { exp: Math.floor(Date.now() / 1000) - 60 };
    expect(isExpired(claims)).toBe(true);
  });

  it("returns false when exp is comfortably in the future", () => {
    const claims: JwtClaims = { exp: Math.floor(Date.now() / 1000) + 3600 };
    expect(isExpired(claims)).toBe(false);
  });

  it("respects leeway", () => {
    const exp = Math.floor(Date.now() / 1000) + 30;
    const claims: JwtClaims = { exp };
    expect(isExpired(claims, 60)).toBe(true);
    expect(isExpired(claims, 10)).toBe(false);
  });

  it("treats missing exp as never-expired", () => {
    expect(isExpired({} as JwtClaims)).toBe(false);
  });
});

describe("secondsUntilExpiry", () => {
  it("returns the positive remainder when token is fresh", () => {
    const claims: JwtClaims = { exp: Math.floor(Date.now() / 1000) + 100 };
    const remaining = secondsUntilExpiry(claims);
    expect(remaining).toBeGreaterThan(95);
    expect(remaining).toBeLessThanOrEqual(100);
  });

  it("returns negative when token is past", () => {
    const claims: JwtClaims = { exp: Math.floor(Date.now() / 1000) - 100 };
    expect(secondsUntilExpiry(claims)).toBeLessThan(0);
  });

  it("returns Infinity when exp is absent", () => {
    expect(secondsUntilExpiry({} as JwtClaims)).toBe(Infinity);
  });
});

describe("JwtClaims.tenant_id", () => {
  it("decodeJwt surfaces tenant_id from the payload", () => {
    const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
    const payload = btoa(
      JSON.stringify({
        sub: "alice",
        tenant_id: "acme",
        exp: Math.floor(Date.now() / 1000) + 3600,
      }),
    );
    const token = `${header}.${payload}.sig`;
    const claims = decodeJwt(token);
    expect(claims?.tenant_id).toBe("acme");
  });

  it("tenant_id is undefined when absent (legacy token)", () => {
    const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
    const payload = btoa(
      JSON.stringify({ sub: "bob", exp: Math.floor(Date.now() / 1000) + 3600 }),
    );
    const token = `${header}.${payload}.sig`;
    const claims = decodeJwt(token);
    expect(claims?.tenant_id).toBeUndefined();
  });
});
