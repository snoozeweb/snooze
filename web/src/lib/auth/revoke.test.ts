import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { authStore } from "./store";
import { writeRefreshToken, writeToken } from "./storage";

function makeToken(sub: string): string {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(JSON.stringify({ sub, exp: Math.floor(Date.now() / 1000) + 3600 }));
  return `${header}.${body}.sig`;
}

type Call = { url: string; init: RequestInit | undefined };

function captureFetch(): Call[] {
  const calls: Call[] = [];
  globalThis.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    calls.push({
      url: typeof input === "string" ? input : input instanceof URL ? input.href : input.url,
      init,
    });
    return Promise.resolve(new Response("", { status: 204 }));
  }) as unknown as typeof fetch;
  return calls;
}

describe("logout revocation", () => {
  beforeEach(() => {
    localStorage.clear();
  });
  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("revokes the stored refresh token server-side", () => {
    // Clearing localStorage alone leaves the refresh token usable for the rest
    // of its lease — a real session that outlives the sign-out.
    writeToken(makeToken("alice"));
    writeRefreshToken("live-refresh");
    const calls = captureFetch();

    authStore.getState().logout();

    expect(calls).toHaveLength(1);
    expect(calls[0]!.url).toBe("/api/v1/login/logout");
    expect(calls[0]!.init?.method).toBe("POST");
    expect(JSON.parse(calls[0]!.init?.body as string)).toEqual({ refresh_token: "live-refresh" });
    // Sign-out navigates away immediately; without keepalive the browser may
    // cancel the request as the page tears down.
    expect(calls[0]!.init?.keepalive).toBe(true);
    expect(localStorage.getItem("snooze-refresh-token")).toBeNull();
    expect(authStore.getState().isAuthenticated).toBe(false);
  });

  it("still signs out locally when the revoke request fails", () => {
    writeToken(makeToken("bob"));
    writeRefreshToken("live-refresh");
    globalThis.fetch = vi.fn(() => Promise.reject(new Error("offline"))) as unknown as typeof fetch;

    expect(() => authStore.getState().logout()).not.toThrow();

    expect(localStorage.getItem("snooze-token")).toBeNull();
    expect(authStore.getState().isAuthenticated).toBe(false);
  });

  it("skips the round trip when the server already rejected the session", () => {
    writeToken(makeToken("carol"));
    writeRefreshToken("already-dead");
    const calls = captureFetch();

    authStore.getState().logout({ revoke: false });

    expect(calls).toHaveLength(0);
    expect(authStore.getState().isAuthenticated).toBe(false);
  });

  it("makes no request when there is no refresh token to revoke", () => {
    writeToken(makeToken("dave"));
    const calls = captureFetch();

    authStore.getState().logout();

    expect(calls).toHaveLength(0);
  });
});
