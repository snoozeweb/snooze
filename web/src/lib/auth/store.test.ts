import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { authStore, useAuth } from "./store";

function makeToken(payload: object): string {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(JSON.stringify(payload));
  return `${header}.${body}.sig`;
}

const FRESH_EXP = Math.floor(Date.now() / 1000) + 3600;
const STALE_EXP = Math.floor(Date.now() / 1000) - 60;

describe("auth store", () => {
  beforeEach(() => {
    localStorage.clear();
    authStore.getState().logout();
  });
  afterEach(() => {
    localStorage.clear();
    authStore.getState().logout();
  });

  it("starts logged out when storage is empty", () => {
    const { result } = renderHook(() => useAuth());
    expect(result.current.isAuthenticated).toBe(false);
    expect(result.current.token).toBeNull();
    expect(result.current.claims).toBeNull();
  });

  it("login sets token + claims + isAuthenticated", () => {
    const tok = makeToken({ sub: "alice", exp: FRESH_EXP });
    const { result } = renderHook(() => useAuth());
    act(() => result.current.login(tok));
    expect(result.current.token).toBe(tok);
    expect(result.current.claims?.sub).toBe("alice");
    expect(result.current.isAuthenticated).toBe(true);
    expect(localStorage.getItem("snooze-token")).toBe(tok);
  });

  it("logout clears everything", () => {
    const tok = makeToken({ sub: "x", exp: FRESH_EXP });
    const { result } = renderHook(() => useAuth());
    act(() => result.current.login(tok));
    act(() => result.current.logout());
    expect(result.current.token).toBeNull();
    expect(result.current.claims).toBeNull();
    expect(result.current.isAuthenticated).toBe(false);
    expect(localStorage.getItem("snooze-token")).toBeNull();
  });

  it("isAuthenticated is false for an expired token", () => {
    const tok = makeToken({ sub: "x", exp: STALE_EXP });
    const { result } = renderHook(() => useAuth());
    act(() => result.current.login(tok));
    expect(result.current.isAuthenticated).toBe(false);
    expect(result.current.token).toBe(tok);
  });

  it("refresh() picks up changes made by another tab", () => {
    const tok = makeToken({ sub: "alice", exp: FRESH_EXP });
    const { result } = renderHook(() => useAuth());
    expect(result.current.isAuthenticated).toBe(false);
    localStorage.setItem("snooze-token", tok);
    expect(result.current.token).toBeNull();
    act(() => result.current.refresh());
    expect(result.current.token).toBe(tok);
    expect(result.current.isAuthenticated).toBe(true);
  });
});
