import { renderHook, act } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useTableSearch } from "./useTableSearch";

type SearchUpdater = (p: Record<string, unknown> | undefined) => Record<string, unknown>;
type NavigateCall = { to: string; search: SearchUpdater };

// Mock TanStack Router so we can drive useSearch/useLocation and capture the
// navigate() args the hook builds — that's where the URL round-trip lives.
// routerCtx is a const holder (initialised before any test) whose fields the
// mock reads at call time, so individual tests can vary the URL state.
const navigateMock = vi.fn<(opts: NavigateCall) => Promise<void>>(() => Promise.resolve());
const routerCtx: { search: Record<string, unknown>; pathname: string } = {
  search: {},
  pathname: "/web/things",
};
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigateMock,
  useSearch: () => routerCtx.search,
  useLocation: () => ({ pathname: routerCtx.pathname }),
}));

/** Run the search-updater the hook passed to navigate() against `prev`. */
function applySearch(prev: Record<string, unknown> | undefined): Record<string, unknown> {
  const lastCall = navigateMock.mock.calls.at(-1)?.[0];
  if (!lastCall) throw new Error("navigate() was never called");
  return lastCall.search(prev);
}

describe("useTableSearch", () => {
  beforeEach(() => {
    navigateMock.mockClear();
    routerCtx.search = {};
    routerCtx.pathname = "/web/things";
  });

  it("seeds the input text from the URL ?search= param", () => {
    routerCtx.search = { search: "host = a" };
    const { result } = renderHook(() => useTableSearch({ collection: "thing" }));
    expect(result.current.text).toBe("host = a");
    expect(result.current.searchProp.value).toBe("host = a");
  });

  it("commits a query to the URL on submit, preserving other params", () => {
    const { result } = renderHook(() => useTableSearch({ collection: "thing" }));
    act(() => result.current.searchProp.onSubmit("host = b"));
    expect(navigateMock).toHaveBeenCalledTimes(1);
    expect(navigateMock.mock.calls[0]?.[0].to).toBe("/web/things");
    expect(applySearch({ page: 2, orderby: "name" })).toEqual({
      page: 2,
      orderby: "name",
      search: "host = b",
    });
  });

  it("drops the ?search= key when the committed query is empty (cleared)", () => {
    const { result } = renderHook(() => useTableSearch({ collection: "thing" }));
    act(() => result.current.searchProp.onSubmit("   "));
    expect(applySearch({ search: "old", page: 3 })).toEqual({ page: 3 });
  });

  it("uses a custom paramKey for both seeding and commit (multi-search pages)", () => {
    routerCtx.search = { aggSearch: "fields CONTAINS host" };
    const { result } = renderHook(() =>
      useTableSearch({ collection: "aggregaterule", paramKey: "aggSearch" }),
    );
    expect(result.current.text).toBe("fields CONTAINS host");
    act(() => result.current.searchProp.onSubmit("fields CONTAINS env"));
    expect(applySearch({ search: "untouched" })).toEqual({
      search: "untouched",
      aggSearch: "fields CONTAINS env",
    });
  });

  it("onChange updates text + condition and derives the encoded ?q=, firing onFilterChange", () => {
    const onFilterChange = vi.fn();
    const { result } = renderHook(() => useTableSearch({ collection: "thing", onFilterChange }));
    act(() =>
      result.current.searchProp.onChange({
        text: "host = c",
        condition: { op: "=", field: "host", value: "c" },
      }),
    );
    expect(result.current.text).toBe("host = c");
    expect(result.current.condition).toEqual({ op: "=", field: "host", value: "c" });
    expect(typeof result.current.q).toBe("string");
    expect(onFilterChange).toHaveBeenCalledTimes(1);
  });

  it("leaves ?q= undefined for an empty / always-true condition", () => {
    const { result } = renderHook(() => useTableSearch({ collection: "thing" }));
    act(() => result.current.searchProp.onChange({ text: "", condition: null }));
    expect(result.current.q).toBeUndefined();
  });

  it("re-seeds the text when the URL param changes from the outside (browser back)", () => {
    const { result, rerender } = renderHook(() => useTableSearch({ collection: "thing" }));
    expect(result.current.text).toBe("");
    act(() => {
      routerCtx.search = { search: "host = ext" };
      rerender();
    });
    expect(result.current.text).toBe("host = ext");
  });
});
