import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, type ApiError } from "@/lib/api/client";
import { Icon } from "@/shared/icons/Icon";
import { tokenize, type Token } from "@/shared/searchdsl/lexer";
import { suggest, type FieldInfo, type Suggestion } from "@/shared/searchdsl/suggest";
import styles from "./SearchBar.module.css";

export type ParsedCondition = {
  op: string;
  field?: string;
  value?: unknown;
  children?: ParsedCondition[];
};

export type ParseError = {
  pos: number;
  token?: string;
  message: string;
};

export type SearchBarChange = {
  /** Raw text shown in the input. */
  text: string;
  /**
   * Parsed AST from the server. Null when the query is empty (AlwaysTrue)
   * or when parsing failed — callers should treat null as "no filter".
   */
  condition: ParsedCondition | null;
  /** Server-reported parse error, if any. */
  error: ParseError | null;
};

export type SearchBarProps = {
  value: string;
  onChange: (next: SearchBarChange) => void;
  /** Backing collection name (drives the field autocomplete catalog). */
  collection?: string;
  placeholder?: string;
  className?: string;
  /** Optional aria-label for the textbox (defaults to "Search"). */
  ariaLabel?: string;
};

type FieldsResponse = { data: FieldInfo[] };
type ParseResponse = {
  condition?: ParsedCondition;
  error?: ParseError;
};

/**
 * SearchBar renders the Snooze search-DSL editor: a single-line text input
 * with live syntax highlighting, server-validated parsing, and a
 * cursor-aware autocomplete popover.
 *
 * Architecture:
 *   - The TS lexer at @/shared/searchdsl/lexer drives the colored overlay
 *     so highlighting stays responsive to keystrokes without a server
 *     round-trip.
 *   - The Go parser at /api/v1/condition/parse is the source of truth for
 *     the AST and any error position. Calls are debounced 250ms.
 *   - The catalog at /api/v1/condition/fields feeds the autocomplete
 *     popover with field names, enum values, and operator hints.
 *
 * The component is uncontrolled w.r.t. the cursor (it lives in the DOM)
 * but controlled w.r.t. the text via `value` / `onChange`. Parent owns the
 * text, the SearchBar owns the parser handshake.
 */
export function SearchBar({
  value,
  onChange,
  collection = "record",
  placeholder = "host = … AND severity = …",
  className,
  ariaLabel = "Search",
}: SearchBarProps) {
  const inputRef = useRef<HTMLInputElement | null>(null);
  const overlayRef = useRef<HTMLDivElement | null>(null);
  const [cursor, setCursor] = useState(0);
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const [error, setError] = useState<ParseError | null>(null);

  // Auto-close the popover after a brief idle period so the suggestions
  // don't permanently cover the rows the user is searching. ArrowDown /
  // Tab / a fresh keystroke re-opens it. Tracked via a ref so we can
  // clear+restart on every event without re-rendering.
  const idleTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const POPOVER_IDLE_MS = 1200;
  const armIdleClose = useCallback(() => {
    if (idleTimerRef.current) clearTimeout(idleTimerRef.current);
    idleTimerRef.current = setTimeout(() => setOpen(false), POPOVER_IDLE_MS);
  }, []);
  const cancelIdleClose = useCallback(() => {
    if (idleTimerRef.current) {
      clearTimeout(idleTimerRef.current);
      idleTimerRef.current = null;
    }
  }, []);
  useEffect(() => {
    return () => {
      if (idleTimerRef.current) clearTimeout(idleTimerRef.current);
    };
  }, []);

  // Field catalog. Cached aggressively — it doesn't change between requests.
  const fields = useQuery<FieldsResponse, ApiError>({
    queryKey: ["condition", "fields", collection],
    queryFn: () =>
      api<FieldsResponse>("GET", "/condition/fields", { query: { collection } }),
    staleTime: 5 * 60_000,
  });

  // Debounced server parse — fires on text change, not on cursor moves.
  useEffect(() => {
    const handle = setTimeout(() => {
      if (!value.trim()) {
        setError(null);
        onChange({ text: value, condition: null, error: null });
        return;
      }
      api<ParseResponse>("POST", "/condition/parse", { body: { query: value } })
        .then((res) => {
          setError(res.error ?? null);
          onChange({
            text: value,
            condition: res.condition ?? null,
            error: res.error ?? null,
          });
        })
        .catch((e: unknown) => {
          // A network failure should not blank the field — keep highlighting,
          // surface the error inline.
          const msg = e instanceof Error ? e.message : "parse failed";
          const err: ParseError = { pos: 0, message: msg };
          setError(err);
          onChange({ text: value, condition: null, error: err });
        });
    }, 250);
    return () => clearTimeout(handle);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- onChange is set on every parent render; including it would re-fire the debounce on every keystroke. The handler is intentionally stable across the lifetime of the SearchBar; parent should memoize if it cares.
  }, [value, collection]);

  // Syntax-highlight tokens for the overlay. Re-runs only when the text
  // changes (cheap; the lexer is O(n) and called once per keystroke).
  const tokens = useMemo(() => tokenize(value), [value]);

  // Compute autocomplete suggestions whenever text or cursor moves.
  const suggestion = useMemo(() => {
    return suggest(value, cursor, fields.data?.data ?? []);
    // We deliberately include `fields.data?.data` so the popover refreshes
    // after the catalog arrives. The reference is stable across queries
    // when the data hasn't changed (react-query gives identical refs).
  }, [value, cursor, fields.data?.data]);

  // Keep the overlay scroll offset aligned with the input's. Without this,
  // typing past the visible width leaves the highlight visibly out of sync
  // with the cursor.
  useLayoutEffect(() => {
    const input = inputRef.current;
    const ov = overlayRef.current;
    if (!input || !ov) return;
    ov.scrollLeft = input.scrollLeft;
  }, [value, cursor]);

  function syncCursor(): void {
    const el = inputRef.current;
    if (!el) return;
    setCursor(el.selectionStart ?? el.value.length);
  }

  function handleChange(e: React.ChangeEvent<HTMLInputElement>): void {
    onChange({ text: e.target.value, condition: null, error });
    // Open the popover as soon as the user types. We re-open on every
    // keystroke even when already open — that's a no-op via setOpen.
    setOpen(true);
    setActiveIndex(0);
    // Restart the idle timer: as long as the user is typing, the popover
    // stays up; once they pause (POPOVER_IDLE_MS), it closes so the
    // table underneath becomes fully visible.
    armIdleClose();
    // The cursor moves on the next tick; defer reading it until then.
    queueMicrotask(syncCursor);
  }

  function handleSelect(s: Suggestion): void {
    const before = value.slice(0, suggestion.replaceFrom);
    const after = value.slice(suggestion.replaceTo);
    // Always include a trailing space after a field, operator or value so
    // typing continues naturally without a manual space.
    const inserted = s.value + (s.kind === "value" || s.kind === "keyword" ? " " : " ");
    const next = before + inserted + after;
    const newCursor = before.length + inserted.length;
    onChange({ text: next, condition: null, error });
    setOpen(true);
    setActiveIndex(0);
    // Focus + caret position need to apply after React commits the new value.
    queueMicrotask(() => {
      const el = inputRef.current;
      if (!el) return;
      el.focus();
      el.setSelectionRange(newCursor, newCursor);
      setCursor(newCursor);
    });
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>): void {
    if (!open && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
      e.preventDefault();
      setOpen(true);
      setActiveIndex(0);
      cancelIdleClose();
      return;
    }
    if (!open) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActiveIndex((i) => Math.min(suggestion.items.length - 1, i + 1));
      cancelIdleClose();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActiveIndex((i) => Math.max(0, i - 1));
      cancelIdleClose();
    } else if (e.key === "Enter") {
      const pick = suggestion.items[activeIndex];
      if (pick) {
        e.preventDefault();
        handleSelect(pick);
      }
    } else if (e.key === "Tab" && suggestion.items.length > 0) {
      const pick = suggestion.items[activeIndex];
      if (pick) {
        e.preventDefault();
        handleSelect(pick);
      }
    } else if (e.key === "Escape") {
      setOpen(false);
      cancelIdleClose();
    }
  }

  const renderTokens = useCallback((): React.ReactNode => {
    // Render a span per token; whitespace between tokens is rendered as
    // a plain text node so width stays in lockstep with the input.
    const nodes: React.ReactNode[] = [];
    let pos = 0;
    for (const t of tokens) {
      if (t.kind === "eof") break;
      if (t.pos > pos) {
        nodes.push(value.slice(pos, t.pos));
      }
      const cls = tokenClass(t);
      nodes.push(
        <span key={`${t.pos}-${t.kind}`} className={cls}>
          {value.slice(t.pos, t.pos + t.len)}
        </span>,
      );
      pos = t.pos + t.len;
    }
    if (pos < value.length) nodes.push(value.slice(pos));
    return nodes;
  }, [tokens, value]);

  const onBlur = useCallback((e: React.FocusEvent<HTMLInputElement>): void => {
    // If focus is moving into the suggestion list or the clear button,
    // keep the popover open (clear-button click would otherwise blur the
    // input and the popover would race-close before we cleared).
    const next = e.relatedTarget as HTMLElement | null;
    if (next && next.closest(`.${styles.popover}`)) return;
    if (next && next.closest(`.${styles.clearBtn}`)) return;
    setOpen(false);
  }, []);

  const handleClear = useCallback(() => {
    setError(null);
    onChange({ text: "", condition: null, error: null });
    setOpen(false);
    queueMicrotask(() => inputRef.current?.focus());
  }, [onChange]);

  return (
    <div className={[styles.wrap, error ? styles.invalid : null, className].filter(Boolean).join(" ")}>
      <span className={styles.leadIcon} aria-hidden="true">
        <Icon name="search" size={14} />
      </span>
      <div className={styles.inputBox}>
        <div ref={overlayRef} className={styles.overlay} aria-hidden="true">
          {value.length > 0 ? renderTokens() : <span className={styles.placeholder}>{placeholder}</span>}
        </div>
        <input
          ref={inputRef}
          className={styles.input}
          spellCheck={false}
          autoComplete="off"
          autoCorrect="off"
          autoCapitalize="off"
          aria-label={ariaLabel}
          {...(error
            ? { "aria-invalid": true as const, "aria-describedby": "searchbar-error" }
            : {})}
          value={value}
          onChange={handleChange}
          onKeyDown={handleKeyDown}
          onKeyUp={syncCursor}
          onClick={syncCursor}
          onSelect={syncCursor}
          onFocus={(e) => {
            setOpen(true);
            setCursor(e.target.selectionStart ?? e.target.value.length);
          }}
          onBlur={onBlur}
        />
      </div>
      {value.length > 0 ? (
        <button
          type="button"
          className={styles.clearBtn}
          aria-label="Clear search"
          // onMouseDown instead of onClick so the action lands BEFORE the
          // input's onBlur fires — otherwise focus shifts to the button and
          // the popover closes mid-click.
          onMouseDown={(e) => {
            e.preventDefault();
            handleClear();
          }}
        >
          <Icon name="x" size={14} />
        </button>
      ) : null}
      {open && suggestion.items.length > 0 ? (
        <div
          className={styles.popover}
          role="listbox"
          tabIndex={-1}
          // Hovering the popover cancels the idle timer so it doesn't close
          // out from under the user while they're reaching for a suggestion.
          onMouseEnter={cancelIdleClose}
          onMouseLeave={armIdleClose}
        >
          <div className={styles.popoverHead}>
            {suggestion.kind === "field" && (suggestion.field ? `Field for ${suggestion.field}` : "Field name")}
            {suggestion.kind === "operator" && "Operator"}
            {suggestion.kind === "value" && (suggestion.field ? `Value for ${suggestion.field}` : "Value")}
            {suggestion.kind === "keyword" && "Combinator"}
          </div>
          <ul className={styles.list}>
            {suggestion.items.map((s, i) => (
              <li
                key={`${s.kind}-${s.value}`}
                role="option"
                aria-selected={i === activeIndex}
                className={[styles.option, i === activeIndex ? styles.optionActive : null]
                  .filter(Boolean)
                  .join(" ")}
                onMouseDown={(e) => {
                  // Prevent blur before click registers.
                  e.preventDefault();
                  handleSelect(s);
                }}
                onMouseEnter={() => setActiveIndex(i)}
              >
                <span className={styles.optionLabel}>{s.label}</span>
                {s.detail ? <span className={styles.optionDetail}>{s.detail}</span> : null}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      {error ? (
        <div id="searchbar-error" className={styles.error} role="status">
          <Icon name="alert-triangle" size={12} />
          <span>
            {error.message}
            {error.token ? ` near "${error.token}"` : ""} (pos {error.pos})
          </span>
        </div>
      ) : null}
    </div>
  );
}

function tokenClass(t: Token): string {
  switch (t.kind) {
    case "string":
      return styles.tString!;
    case "number":
      return styles.tNumber!;
    case "bool":
      return styles.tBool!;
    case "and":
    case "or":
    case "not":
    case "matches":
    case "exists_kw":
    case "contains":
    case "in":
      return styles.tKeyword!;
    case "eq":
    case "neq":
    case "lt":
    case "lte":
    case "gt":
    case "gte":
    case "exists_sym":
      return styles.tOperator!;
    case "lparen":
    case "rparen":
    case "lbrack":
    case "rbrack":
    case "lbrace":
    case "rbrace":
    case "comma":
    case "colon":
      return styles.tPunct!;
    case "error":
      return styles.tError!;
    default:
      return styles.tIdent!;
  }
}
