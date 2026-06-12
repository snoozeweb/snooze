import { renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useShortcut } from "./useShortcut";

describe("useShortcut", () => {
  it("invokes the handler on the matching keydown", () => {
    const handler = vi.fn();
    renderHook(() => useShortcut("mod+k", handler));
    const ev = new KeyboardEvent("keydown", { key: "k", ctrlKey: true, metaKey: false });
    window.dispatchEvent(ev);
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("ignores keydowns that don't match", () => {
    const handler = vi.fn();
    renderHook(() => useShortcut("mod+k", handler));
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "j", ctrlKey: true }));
    expect(handler).not.toHaveBeenCalled();
  });

  it("ignores when focus is inside an input by default", () => {
    const handler = vi.fn();
    renderHook(() => useShortcut("mod+k", handler));
    const input = document.createElement("input");
    document.body.appendChild(input);
    input.focus();
    input.dispatchEvent(new KeyboardEvent("keydown", { key: "k", ctrlKey: true, bubbles: true }));
    expect(handler).not.toHaveBeenCalled();
    input.remove();
  });

  it("fires inside inputs when enableInInputs=true", () => {
    const handler = vi.fn();
    renderHook(() => useShortcut("mod+k", handler, { enableInInputs: true }));
    const input = document.createElement("input");
    document.body.appendChild(input);
    input.focus();
    input.dispatchEvent(new KeyboardEvent("keydown", { key: "k", ctrlKey: true, bubbles: true }));
    expect(handler).toHaveBeenCalled();
    input.remove();
  });

  // ── Registry / shared-listener tests ──────────────────────────────────────

  describe("shared listener", () => {
    afterEach(() => {
      // Nothing to clean up here — renderHook unmount handles deregistration.
    });

    it("two components binding different combos share one window listener", () => {
      const spy = vi.spyOn(window, "addEventListener");

      const h1 = vi.fn();
      const h2 = vi.fn();
      const { unmount: u1 } = renderHook(() => useShortcut("mod+k", h1));
      const { unmount: u2 } = renderHook(() => useShortcut("mod+j", h2));

      // Only one keydown listener should have been added across both hooks.
      const keydownCalls = spy.mock.calls.filter(([ev]) => ev === "keydown");
      expect(keydownCalls.length).toBe(1);

      // Both handlers still fire correctly.
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "k", ctrlKey: true }));
      expect(h1).toHaveBeenCalledTimes(1);
      expect(h2).not.toHaveBeenCalled();

      window.dispatchEvent(new KeyboardEvent("keydown", { key: "j", ctrlKey: true }));
      expect(h2).toHaveBeenCalledTimes(1);

      u1();
      u2();
      spy.mockRestore();
    });

    it("handler updates take effect without re-subscription", () => {
      const first = vi.fn();
      const second = vi.fn();
      // Start with `first` as the handler.
      const { rerender } = renderHook(({ h }) => useShortcut("mod+p", h), {
        initialProps: { h: first },
      });

      window.dispatchEvent(new KeyboardEvent("keydown", { key: "p", ctrlKey: true }));
      expect(first).toHaveBeenCalledTimes(1);
      expect(second).not.toHaveBeenCalled();

      // Swap to `second` — the registry must not re-register.
      const addSpy = vi.spyOn(window, "addEventListener");
      rerender({ h: second });
      expect(addSpy).not.toHaveBeenCalled();
      addSpy.mockRestore();

      window.dispatchEvent(new KeyboardEvent("keydown", { key: "p", ctrlKey: true }));
      // Only the new handler fires; the old closure is gone.
      expect(first).toHaveBeenCalledTimes(1);
      expect(second).toHaveBeenCalledTimes(1);
    });

    it("unmount removes the binding", () => {
      const handler = vi.fn();
      const { unmount } = renderHook(() => useShortcut("mod+q", handler));

      window.dispatchEvent(new KeyboardEvent("keydown", { key: "q", ctrlKey: true }));
      expect(handler).toHaveBeenCalledTimes(1);

      unmount();

      window.dispatchEvent(new KeyboardEvent("keydown", { key: "q", ctrlKey: true }));
      // Should NOT fire again after unmount.
      expect(handler).toHaveBeenCalledTimes(1);
    });

    it("last unmount removes the shared window listener", () => {
      const removeSpy = vi.spyOn(window, "removeEventListener");

      const { unmount: u1 } = renderHook(() => useShortcut("mod+1", vi.fn()));
      const { unmount: u2 } = renderHook(() => useShortcut("mod+2", vi.fn()));

      // First unmount must NOT remove the listener (u2 still active).
      u1();
      const removedBeforeU2 = removeSpy.mock.calls.filter(([ev]) => ev === "keydown");
      expect(removedBeforeU2.length).toBe(0);

      // Last unmount MUST remove the shared listener.
      u2();
      const removedAfter = removeSpy.mock.calls.filter(([ev]) => ev === "keydown");
      expect(removedAfter.length).toBe(1);

      removeSpy.mockRestore();
    });

    it("two bindings for same combo with different enableInInputs evaluate per-binding", () => {
      const handlerNoInput = vi.fn();
      const handlerWithInput = vi.fn();

      renderHook(() => useShortcut("mod+m", handlerNoInput, { enableInInputs: false }));
      renderHook(() => useShortcut("mod+m", handlerWithInput, { enableInInputs: true }));

      const input = document.createElement("input");
      document.body.appendChild(input);
      input.focus();
      input.dispatchEvent(new KeyboardEvent("keydown", { key: "m", ctrlKey: true, bubbles: true }));

      // Only the enableInInputs:true binding fires.
      expect(handlerNoInput).not.toHaveBeenCalled();
      expect(handlerWithInput).toHaveBeenCalledTimes(1);
      input.remove();
    });
  });
});
