import { useCallback, useMemo, useState } from "react";
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
};

/**
 * useTableSearch — share-the-burden of wiring the SearchBar into a list
 * page. The hook owns the (text, condition) state, derives the encoded
 * `?q=` value, and exposes a `searchProp` object ready to splat onto
 * DataTable's `search` prop. Every list page that wants server-side
 * filtering can adopt this in 3 lines instead of repeating the encode
 * dance.
 */
export function useTableSearch({
  collection,
  placeholder,
  onFilterChange,
}: UseTableSearchOptions): TableSearchState {
  const [text, setText] = useState("");
  const [condition, setCondition] = useState<ParsedCondition | null>(null);

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

  return {
    text,
    q,
    condition,
    searchProp: {
      value: text,
      onChange,
      collection,
      ...(placeholder ? { placeholder } : {}),
    },
  };
}
