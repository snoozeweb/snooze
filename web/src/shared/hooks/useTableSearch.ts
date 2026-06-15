import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate, useSearch } from "@tanstack/react-router";
import type { ParsedCondition } from "@/shared/ui/SearchBar";
import { encodeConditionQ } from "@/lib/condition/serialize";
import type { Condition } from "@/lib/condition/types";

export type TableSearchState = {
  /** Raw text in the search input. */
  text: string;
  /** Encoded `?q=` value to thread into resource.useList. Undefined when
   *  the search is empty so react-query can share the unfiltered cache. */
  q: string | undefined;
  /** Parsed AST returned by the SearchBar's last server round-trip. */
  condition: ParsedCondition | null;
  /** Slot to pass straight to DataTable's `search` prop. */
  searchProp: {
    value: string;
    onChange: (next: { text: string; condition: ParsedCondition | null }) => void;
    onSubmit: (text: string) => void;
    collection: string;
    placeholder?: string;
  };
};

export type UseTableSearchOptions = {
  /** Field-catalog collection name (rule, snooze, user, …). */
  collection: string;
  /** Placeholder hint shown in the SearchBar input. */
  placeholder?: string;
  /** Called when the filter changes — pages typically use it to reset
   *  pagination to page 1 so the new filter isn't viewed past its end. */
  onFilterChange?: () => void;
  /**
   * URL search-param key the committed query round-trips through. Defaults to
   * `search`. Pages with two search bars (Rules, Notifications) give the
   * second one a distinct key so the two queries don't collide in the URL.
   */
  paramKey?: string;
};

/**
 * useTableSearch — share-the-burden of wiring the SearchBar into a list
 * page. The hook owns the (text, condition) state, derives the encoded
 * `?q=` value, and exposes a `searchProp` object ready to splat onto
 * DataTable's `search` prop. Every list page that wants server-side
 * filtering can adopt this in 3 lines instead of repeating the encode
 * dance.
 *
 * The committed query also round-trips through the URL (`?<paramKey>=`) so a
 * filtered view is shareable and survives a reload — the same contract the
 * Alerts page uses. URL → state seeding happens on mount and on external URL
 * changes; state → URL happens only on a discrete commit (Enter / clear) via
 * the SearchBar's `onSubmit`, never per-keystroke — navigate() is async and
 * the round trip drops characters on fast typing. The owning route's
 * `validateSearch` must preserve `paramKey`, or it gets stripped on the next
 * navigation.
 */
export function useTableSearch({
  collection,
  placeholder,
  onFilterChange,
  paramKey = "search",
}: UseTableSearchOptions): TableSearchState {
  // useSearch returns the merged union of every route's params (a fully-known
  // object, not an index-signature record), so widen through unknown to read a
  // dynamic paramKey — same cast idiom the navigate calls use below.
  const urlSearch = useSearch({ strict: false }) as unknown as Record<string, unknown>;
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const rawValue = urlSearch[paramKey];
  const urlValue = typeof rawValue === "string" ? rawValue : undefined;

  const [text, setText] = useState<string>(() => urlValue ?? "");
  const [condition, setCondition] = useState<ParsedCondition | null>(null);

  // Re-seed local text when the URL value changes from the outside (browser
  // back/forward, a deep link). Pure typing leaves the URL untouched, so this
  // never clobbers the draft mid-edit.
  const lastSeededRef = useRef<string | undefined>(urlValue);
  useEffect(() => {
    if (urlValue !== lastSeededRef.current) {
      lastSeededRef.current = urlValue;
      setText(urlValue ?? "");
    }
  }, [urlValue]);

  const q = useMemo(() => {
    if (!condition || condition.op === "" || condition.op === "ALWAYS_TRUE") {
      return undefined;
    }
    return encodeConditionQ(condition as unknown as Condition);
  }, [condition]);

  const onChange = useCallback(
    (next: { text: string; condition: ParsedCondition | null }) => {
      setText(next.text);
      setCondition(next.condition);
      onFilterChange?.();
    },
    [onFilterChange],
  );

  const onSubmit = useCallback(
    (committed: string) => {
      const trimmed = committed.trim();
      // navigate's types are pinned to the registered route tree; the unknown
      // cast lets this route-agnostic hook update one search key on whatever
      // route it's mounted under (to: the current pathname). Building a plain
      // record and deleting the empty key sidesteps exactOptionalPropertyTypes
      // and matches how TanStack Router drops undefined keys from the URL.
      type NavigateFn = (opts: {
        to: string;
        search: (prev: Record<string, unknown> | undefined) => Record<string, unknown>;
      }) => Promise<void>;
      void (navigate as unknown as NavigateFn)({
        to: pathname,
        search: (prev) => {
          const merged: Record<string, unknown> = { ...(prev ?? {}) };
          if (trimmed) merged[paramKey] = committed;
          else delete merged[paramKey];
          return merged;
        },
      });
    },
    [navigate, pathname, paramKey],
  );

  return {
    text,
    q,
    condition,
    searchProp: {
      value: text,
      onChange,
      onSubmit,
      collection,
      ...(placeholder ? { placeholder } : {}),
    },
  };
}
