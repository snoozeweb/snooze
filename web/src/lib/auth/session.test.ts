import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  ensureFreshToken,
  ensureRotation,
  startSessionRefresh,
  stopSessionRefresh,
} from "./session";
import { authStore } from "./store";
import { readRefreshToken, readToken, writeRefreshToken, writeToken } from "./storage";

function tokenExpiringIn(seconds: number, sub = "alice"): string {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(JSON.stringify({ sub, exp: Math.floor(Date.now() / 1000) + seconds }));
  return `${header}.${body}.sig`;
}

function mockFetch(handler: (url: string) => Response | Promise<Response>) {
  const fn = vi.fn((input: RequestInfo | URL) =>
    Promise.resolve(
      handler(typeof input === "string" ? input : input instanceof URL ? input.href : input.url),
    ),
  );
  globalThis.fetch = fn as unknown as typeof fetch;
  return fn;
}

const refreshOk = (token: string, refresh = "next-refresh") =>
  new Response(JSON.stringify({ token, refresh_token: refresh }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });

describe("ensureRotation", () => {
  beforeEach(() => {
    localStorage.clear();
    authStore.getState().logout();
  });
  afterEach(() => {
    stopSessionRefresh();
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("rotates, persists both tokens, and updates the auth store", async () => {
    writeToken(tokenExpiringIn(-10));
    writeRefreshToken("seed");
    const rotated = tokenExpiringIn(3600, "alice-rotated");
    mockFetch(() => refreshOk(rotated));

    const out = await ensureRotation();

    expect(out).toEqual({ token: rotated, fatal: false });
    expect(readToken()).toBe(rotated);
    expect(readRefreshToken()).toBe("next-refresh");
    // The store must move with storage: leaving it on the old expired claims
    // is what made route guards bounce a session that had just been renewed.
    expect(authStore.getState().token).toBe(rotated);
    expect(authStore.getState().claims?.sub).toBe("alice-rotated");
    expect(authStore.getState().isAuthenticated).toBe(true);
  });

  it("reports a rejected refresh token as fatal", async () => {
    writeToken(tokenExpiringIn(-10));
    writeRefreshToken("revoked");
    mockFetch(() => new Response("", { status: 401 }));

    await expect(ensureRotation()).resolves.toEqual({ token: null, fatal: true });
  });

  it("treats an unreachable or broken server as transient, not fatal", async () => {
    writeToken(tokenExpiringIn(-10));
    writeRefreshToken("seed");
    mockFetch(() => new Response("", { status: 502 }));
    await expect(ensureRotation()).resolves.toEqual({ token: null, fatal: false });

    globalThis.fetch = vi.fn(() => Promise.reject(new Error("offline"))) as unknown as typeof fetch;
    await expect(ensureRotation()).resolves.toEqual({ token: null, fatal: false });
  });

  it("collapses concurrent callers into a single /refresh request", async () => {
    writeToken(tokenExpiringIn(-10));
    writeRefreshToken("seed");
    const rotated = tokenExpiringIn(3600);
    const fetchSpy = mockFetch(() => refreshOk(rotated));

    const results = await Promise.all([ensureRotation(), ensureRotation(), ensureRotation()]);

    expect(fetchSpy).toHaveBeenCalledTimes(1);
    for (const r of results) expect(r.token).toBe(rotated);
  });

  it("does nothing when there is no refresh token to present", async () => {
    writeToken(tokenExpiringIn(-10));
    const fetchSpy = mockFetch(() => refreshOk("nope"));
    await expect(ensureRotation()).resolves.toEqual({ token: null, fatal: false });
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("steps aside while another tab holds the rotation lock", async () => {
    // A live lock written by a peer tab. Rotating the same refresh token twice
    // revokes it server-side, so this tab must wait for the peer's result.
    localStorage.setItem("snooze-refresh-lock", `peer:${Date.now()}`);
    writeToken(tokenExpiringIn(-10));
    writeRefreshToken("seed");
    const fetchSpy = mockFetch(() => refreshOk("unused"));

    const peerToken = tokenExpiringIn(3600, "from-peer");
    const pending = ensureRotation();
    setTimeout(() => {
      writeToken(peerToken);
      localStorage.removeItem("snooze-refresh-lock");
    }, 150);

    await expect(pending).resolves.toEqual({ token: peerToken, fatal: false });
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});

describe("ensureFreshToken", () => {
  beforeEach(() => {
    localStorage.clear();
    authStore.getState().logout();
  });
  afterEach(() => {
    stopSessionRefresh();
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("returns the current token untouched when it is comfortably valid", async () => {
    const token = tokenExpiringIn(3600);
    writeToken(token);
    writeRefreshToken("seed");
    const fetchSpy = mockFetch(() => refreshOk("unused"));

    await expect(ensureFreshToken()).resolves.toBe(token);
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("rotates ahead of expiry rather than waiting for a 401", async () => {
    writeToken(tokenExpiringIn(20)); // inside the 60s lead window
    writeRefreshToken("seed");
    const rotated = tokenExpiringIn(3600);
    const fetchSpy = mockFetch(() => refreshOk(rotated));

    await expect(ensureFreshToken()).resolves.toBe(rotated);
    expect(fetchSpy).toHaveBeenCalledTimes(1);
  });

  it("keeps a still-valid token when rotation fails transiently", async () => {
    const token = tokenExpiringIn(20);
    writeToken(token);
    writeRefreshToken("seed");
    mockFetch(() => new Response("", { status: 502 }));

    // Not expired yet — a flaky refresh must not cost the operator the session.
    await expect(ensureFreshToken()).resolves.toBe(token);
  });

  it("returns null when the token is expired and rotation is rejected", async () => {
    writeToken(tokenExpiringIn(-10));
    writeRefreshToken("revoked");
    mockFetch(() => new Response("", { status: 401 }));

    await expect(ensureFreshToken()).resolves.toBeNull();
  });

  it("returns null when there is no session at all", async () => {
    await expect(ensureFreshToken()).resolves.toBeNull();
  });
});

describe("startSessionRefresh", () => {
  beforeEach(() => {
    localStorage.clear();
    authStore.getState().logout();
  });
  afterEach(() => {
    stopSessionRefresh();
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("tops the token up when the tab becomes visible after a long sleep", async () => {
    writeToken(tokenExpiringIn(-10));
    writeRefreshToken("seed");
    authStore.getState().refresh();
    const rotated = tokenExpiringIn(3600, "after-wake");
    const fetchSpy = mockFetch(() => refreshOk(rotated));

    startSessionRefresh();
    // A suspended machine wakes with its timer already overdue; the
    // visibilitychange is what actually rescues the session.
    document.dispatchEvent(new Event("visibilitychange"));
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalled());
    await vi.waitFor(() => expect(readToken()).toBe(rotated));
  });
});
