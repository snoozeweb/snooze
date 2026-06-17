import { renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useIsMobileShell } from "./useIsMobileShell";

function mockMatchMedia(matches: boolean) {
  const listeners = new Set<() => void>();
  vi.stubGlobal(
    "matchMedia",
    vi.fn().mockImplementation((query: string) => ({
      matches,
      media: query,
      addEventListener: (_: string, cb: () => void) => listeners.add(cb),
      removeEventListener: (_: string, cb: () => void) => listeners.delete(cb),
    })),
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("useIsMobileShell", () => {
  it("returns true when the mobile media query matches", () => {
    mockMatchMedia(true);
    const { result } = renderHook(() => useIsMobileShell());
    expect(result.current).toBe(true);
  });

  it("returns false when it does not match", () => {
    mockMatchMedia(false);
    const { result } = renderHook(() => useIsMobileShell());
    expect(result.current).toBe(false);
  });

  it("defaults to false (desktop) when matchMedia is unavailable", () => {
    vi.stubGlobal("matchMedia", undefined);
    const { result } = renderHook(() => useIsMobileShell());
    expect(result.current).toBe(false);
  });
});
