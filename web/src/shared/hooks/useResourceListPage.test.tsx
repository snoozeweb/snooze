import { renderHook, act } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useResourceListPage, type BaseListSearch } from "./useResourceListPage";
import type { ContextMenuItem } from "@/shared/ui/DataTableContextMenu";

type NavigateCall = {
  to: string;
  search: (p: ItemSearch | undefined) => Record<string, unknown>;
};

// Mock TanStack Router's useNavigate so we can capture the navigate() args the
// hook builds — that's where the undefined-stripping idiom lives.
const navigateMock = vi.fn<(opts: NavigateCall) => Promise<void>>(() => Promise.resolve());
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigateMock,
}));

type Item = { uid?: string; name: string };
// `tab` carries `| undefined` (as real pages' search types do for optional
// tab/dict keys) so the undefined-stripping path can be exercised.
type ItemSearch = BaseListSearch & { tab?: string | undefined };

function setup(over?: { contextMenuExtras?: (row: Item) => ContextMenuItem[] }) {
  const remove = { mutateAsync: vi.fn().mockResolvedValue(undefined) };
  const hook = renderHook(() =>
    useResourceListPage<Item, ItemSearch>({
      to: "/web/things",
      remove,
      noun: "thing",
      ...(over?.contextMenuExtras ? { contextMenuExtras: over.contextMenuExtras } : {}),
    }),
  );
  return { hook, remove };
}

/** Run the search-updater the hook passed to navigate() against `prev`. */
function applySearch(prev: ItemSearch | undefined): Record<string, unknown> {
  const lastCall = navigateMock.mock.calls.at(-1)?.[0];
  if (!lastCall) throw new Error("navigate() was never called");
  return lastCall.search(prev);
}

describe("useResourceListPage", () => {
  beforeEach(() => navigateMock.mockClear());

  it("updateSearch merges into prev and targets the page route", () => {
    const { hook } = setup();
    act(() => hook.result.current.updateSearch({ page: 3 }));
    expect(navigateMock).toHaveBeenCalledTimes(1);
    expect(navigateMock.mock.calls[0]?.[0].to).toBe("/web/things");
    expect(applySearch({ orderby: "name", page: 1 })).toEqual({ orderby: "name", page: 3 });
  });

  it("updateSearch strips keys explicitly set to undefined", () => {
    const { hook } = setup();
    // Closing the detail panel: uid -> undefined should drop the key entirely.
    act(() => hook.result.current.updateSearch({ uid: undefined }));
    expect(applySearch({ uid: "x1", page: 2 })).toEqual({ page: 2 });
  });

  it("updateSearch drops any undefined key (e.g. returning to the All tab)", () => {
    const { hook } = setup();
    act(() => hook.result.current.updateSearch({ tab: undefined, page: 1 }));
    expect(applySearch({ tab: "local", page: 5, orderby: "name" })).toEqual({
      page: 1,
      orderby: "name",
    });
  });

  it("openRow writes the row uid to the URL", () => {
    const { hook } = setup();
    act(() => hook.result.current.openRow("abc"));
    expect(applySearch(undefined)).toEqual({ uid: "abc" });
  });

  it("keeps callbacks identity-stable across re-renders (row-memo contract)", () => {
    const { hook } = setup();
    const first = {
      updateSearch: hook.result.current.updateSearch,
      contextMenuItems: hook.result.current.contextMenuItems,
      bulkActions: hook.result.current.bulkActions,
      setSelectedKeys: hook.result.current.setSelectedKeys,
      openRow: hook.result.current.openRow,
    };
    hook.rerender();
    expect(hook.result.current.updateSearch).toBe(first.updateSearch);
    expect(hook.result.current.contextMenuItems).toBe(first.contextMenuItems);
    expect(hook.result.current.bulkActions).toBe(first.bulkActions);
    expect(hook.result.current.setSelectedKeys).toBe(first.setSelectedKeys);
    expect(hook.result.current.openRow).toBe(first.openRow);
  });

  it("selection state round-trips through setSelectedKeys", () => {
    const { hook } = setup();
    expect(hook.result.current.selectedKeys.size).toBe(0);
    act(() => hook.result.current.setSelectedKeys(new Set(["a", "b"])));
    expect([...hook.result.current.selectedKeys]).toEqual(["a", "b"]);
  });

  it("contextMenuItems no longer wires Open (rows open on click) but keeps copy + Delete", () => {
    const { hook } = setup();
    const items = hook.result.current.contextMenuItems({ uid: "u9", name: "n" });
    // "Open" was dropped — clicking the row already opens it, so the duplicate
    // menu entry is gone.
    expect(items.find((i) => i.key === "open")).toBeUndefined();
    expect(items.find((i) => i.key === "copy-json")).toBeDefined();
    const del = items.find((i) => i.key === "delete");
    expect(del).toBeDefined();

    // Requesting delete opens the confirm dialog with the row queued.
    act(() => {
      void del?.onSelect();
    });
    expect(hook.result.current.confirmDelete.state?.rows).toEqual([{ uid: "u9", name: "n" }]);
  });

  it("threads contextMenuExtras between the copy actions and Delete", () => {
    const { hook } = setup({
      contextMenuExtras: () => [{ key: "retro-apply", label: "Retro apply", onSelect: () => {} }],
    });
    const items = hook.result.current.contextMenuItems({ uid: "u1", name: "n" });
    const keys = items.map((i) => i.key);
    expect(keys).toContain("retro-apply");
    // Extras land before the trailing Delete item.
    expect(keys.indexOf("retro-apply")).toBeLessThan(keys.indexOf("delete"));
  });

  it("confirmDelete.confirm deletes each row's uid via remove.mutateAsync", async () => {
    const { hook, remove } = setup();
    act(() => hook.result.current.confirmDelete.request([{ uid: "u1", name: "a" }]));
    await act(async () => {
      await hook.result.current.confirmDelete.confirm();
    });
    expect(remove.mutateAsync).toHaveBeenCalledWith("u1");
    // Selection is cleared after the delete completes (onAfter).
    expect(hook.result.current.selectedKeys.size).toBe(0);
  });
});
