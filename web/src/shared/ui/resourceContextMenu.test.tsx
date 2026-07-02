import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/lib/api/client";
import { toastStore } from "@/shared/ui/toast/useToast";
import { useConfirmDelete } from "./resourceContextMenu";

afterEach(() => toastStore.clear());

describe("useConfirmDelete", () => {
  it("on partial failure surfaces the backend's reason and re-selects only the failed rows", async () => {
    // Backends reject some deletes with a real reason (e.g. GuardDelete: the
    // last platform_admin). Swallowing it as "1 of 2 failed" leaves the operator
    // guessing which row and why, and wiping the whole selection forces a re-scan.
    const onAfter = vi.fn();
    const onDelete = (uid: string) =>
      uid === "u2"
        ? Promise.reject(
            new ApiError(403, "guarded", "cannot delete the last enabled platform_admin"),
          )
        : Promise.resolve({});
    const { result } = renderHook(() =>
      useConfirmDelete<{ uid?: string; name?: string }>({ onDelete, noun: "user", onAfter }),
    );
    act(() =>
      result.current.request([
        { uid: "u1", name: "alice" },
        { uid: "u2", name: "bob" },
      ]),
    );
    await act(async () => {
      await result.current.confirm();
    });
    // Only the failed row is passed back so the caller can keep it selected.
    expect(onAfter).toHaveBeenCalledWith([{ uid: "u2", name: "bob" }]);
    const toasts = toastStore.getSnapshot();
    expect(
      toasts.some((t) => /cannot delete the last enabled platform_admin/i.test(t.description)),
    ).toBe(true);
  });

  it("uses a caller-provided describe() for the confirm title/message", () => {
    const { result } = renderHook(() =>
      useConfirmDelete<{ uid?: string; name?: string }>({
        onDelete: () => Promise.resolve({}),
        noun: "tenant",
        describe: (rows) => ({
          title: `Delete tenant ${rows[0]?.name ?? ""}?`,
          message: "All of its data becomes inaccessible. This cannot be undone.",
        }),
      }),
    );
    act(() => result.current.request([{ uid: "t1", name: "acme" }]));
    expect(result.current.state?.title).toBe("Delete tenant acme?");
    expect(result.current.state?.message).toMatch(/becomes inaccessible/i);
  });

  it("on full success calls onAfter with an empty list so the selection clears", async () => {
    const onAfter = vi.fn();
    const { result } = renderHook(() =>
      useConfirmDelete<{ uid?: string }>({
        onDelete: () => Promise.resolve({}),
        noun: "user",
        onAfter,
      }),
    );
    act(() => result.current.request([{ uid: "u1" }, { uid: "u2" }]));
    await act(async () => {
      await result.current.confirm();
    });
    expect(onAfter).toHaveBeenCalledWith([]);
  });
});
