import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
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
});
