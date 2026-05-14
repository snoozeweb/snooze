import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { toast, toastStore, useToasts } from "./useToast";

describe("toast store", () => {
  afterEach(() => {
    toastStore.clear();
  });

  it("push adds a toast with a generated id", () => {
    const { result } = renderHook(() => useToasts());
    expect(result.current).toHaveLength(0);
    let id = "";
    act(() => {
      id = toast.success("Saved");
    });
    expect(result.current).toHaveLength(1);
    expect(result.current[0]!.id).toBe(id);
    expect(result.current[0]!.variant).toBe("success");
    expect(result.current[0]!.description).toBe("Saved");
  });

  it("dismiss removes a toast by id", () => {
    const { result } = renderHook(() => useToasts());
    let id = "";
    act(() => {
      id = toast.error("Boom");
    });
    expect(result.current).toHaveLength(1);
    act(() => {
      toast.dismiss(id);
    });
    expect(result.current).toHaveLength(0);
  });

  it("error toasts default to 8s, success to 3s", () => {
    act(() => {
      toast.success("ok");
      toast.error("bad");
    });
    const snapshot = toastStore.getSnapshot();
    expect(snapshot.find((t) => t.variant === "success")!.duration).toBe(3000);
    expect(snapshot.find((t) => t.variant === "error")!.duration).toBe(8000);
  });
});
