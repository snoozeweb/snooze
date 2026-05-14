import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { useAutoRefresh } from "./useAutoRefresh";

describe("useAutoRefresh", () => {
  beforeEach(() => {
    localStorage.clear();
  });
  afterEach(() => {
    localStorage.clear();
  });

  it("defaults to enabled when nothing is stored", () => {
    const { result } = renderHook(() => useAutoRefresh(5000));
    expect(result.current.enabled).toBe(true);
    expect(result.current.intervalMs).toBe(5000);
  });

  it("respects stored 'false'", () => {
    localStorage.setItem("alerts.autoRefresh", "false");
    const { result } = renderHook(() => useAutoRefresh(5000));
    expect(result.current.enabled).toBe(false);
    expect(result.current.intervalMs).toBeUndefined();
  });

  it("setEnabled(false) persists and disables", () => {
    const { result } = renderHook(() => useAutoRefresh(5000));
    act(() => result.current.setEnabled(false));
    expect(result.current.enabled).toBe(false);
    expect(result.current.intervalMs).toBeUndefined();
    expect(localStorage.getItem("alerts.autoRefresh")).toBe("false");
  });

  it("setEnabled(true) persists and re-enables", () => {
    localStorage.setItem("alerts.autoRefresh", "false");
    const { result } = renderHook(() => useAutoRefresh(5000));
    act(() => result.current.setEnabled(true));
    expect(result.current.enabled).toBe(true);
    expect(result.current.intervalMs).toBe(5000);
    expect(localStorage.getItem("alerts.autoRefresh")).toBe("true");
  });
});
