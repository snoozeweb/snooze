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
  /**
   * Fired when the user *commits* the query — pressing Enter with no
   * autocomplete suggestion highlighted, or clearing the field. The text is
   * only forwarded once it parses cleanly (an invalid query keeps its inline
   * error and is not committed); clearing always commits the empty string.
   * Parents typically persist this to the URL so the search is shareable and
   * survives a reload, without paying the per-keystroke navigate() cost.
   */
  onSubmit?: (text: string) => void;
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
 * The component is uncontrolled w.r.t. the cursor (it lives in the DOM) and
 * owns the *draft* text locally so per-keystroke typing never re-renders the
 * parent. `value` seeds the draft and re-seeds it on external changes (chip
 * dismissal, clear-all); `onChange` fires only at parse-resolution cadence —
 * carrying the previously-parsed condition while the user is mid-type, and a
 * fresh condition (or null on empty) once a parse resolves. Parent stores
 * that condition; the SearchBar owns the parser handshake.
 *
 * `onSubmit` (optional) is the *commit* signal, separate from the typing
 * cadence: it fires when the user presses Enter without an autocomplete
 * suggestion highlighted (or clears the field), and only carries text that
 * parsed cleanly. Parents use it to persist the query to the URL on a discrete
 * action, side-stepping the dropped-keystroke problem of per-keystroke
 * navigation.
 */
export function SearchBar({
  value,
  onChange,
  onSubmit,
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

  // Draft text lives here, not in the parent. Typing mutates only this local
  // state, so the parent re-renders at parse cadence rather than per
  // keystroke. The `value` prop seeds it and re-seeds on external changes
  // (chip dismissal, clear-all) — detected by comparing against the value we
  // last folded in. Pure typing leaves `value` untouched, so it doesn't
  // clobber the draft.
  const [draft, setDraft] = useState(value);
  const lastSyncedValueRef = useRef(value);
  if (value !== lastSyncedValueRef.current) {
    lastSyncedValueRef.current = value;
    if (value !== draft) setDraft(value);
  }

  // Latest-ref for onChange: the debounce effect reads through this instead
  // of closing over the prop, so a parent that passes a fresh onChange every
  // render doesn't re-fire the debounce — and the resolved parse always calls
  // the current handler, never a stale one. (React 19 has no useEffectEvent
  // yet, so a ref is the idiomatic stand-in.)
  const onChangeRef = useRef(onChange);
  useEffect(() => {
    onChangeRef.current = onChange;
  });
  // Same latest-ref treatment for onSubmit so the Enter/clear commit path
  // always calls the current handler without re-creating callbacks per render.
  const onSubmitRef = useRef(onSubmit);
  useEffect(() => {
    onSubmitRef.current = onSubmit;
  });

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
    queryFn: () => api<FieldsResponse>("GET", "/condition/fields", { query: { collection } }),
    staleTime: 5 * 60_000,
  });

  // Parse `text` against the server and emit the result through onChange.
  // Shared by the debounced typing path and the Enter/clear commit path, so
  // there is exactly one place that talks to /condition/parse. Returns the
  // parse error (or `null` when the query is valid or empty) so the caller can
  // decide whether to commit. Aborted requests resolve to `null` and emit
  // nothing — the `signal.aborted` guards short-circuit any callback that
  // slips through after cleanup, so a slow parse for an older draft can't land
  // stale text/condition (symptom: "characters get deleted when typing fast").
  const runParse = useCallback(
    async (text: string, signal: AbortSignal): Promise<ParseError | null> => {
      if (!text.trim()) {
        setError(null);
        onChangeRef.current({ text, condition: null, error: null });
        return null;
      }
      try {
        const res = await api<ParseResponse>("POST", "/condition/parse", {
          body: { query: text },
          signal,
        });
        if (signal.aborted) return null;
        const err = res.error ?? null;
        setError(err);
        onChangeRef.current({ text, condition: res.condition ?? null, error: err });
        return err;
      } catch (e: unknown) {
        if (signal.aborted) return null;
        // A network failure should not blank the field — keep highlighting,
        // surface the error inline.
        const msg = e instanceof Error ? e.message : "parse failed";
        const err: ParseError = { pos: 0, message: msg };
        setError(err);
        onChangeRef.current({ text, condition: null, error: err });
        return err;
      }
    },
    [],
  );

  // Debounced server parse — fires on draft text change, not on cursor moves.
  //
  // Cadence contract: while the user types, the previously-parsed condition
  // stays in effect (we DON'T emit per keystroke), so consumers that derive a
  // list-query key from the condition don't flip the table to the unfiltered
  // list mid-refinement. A new condition is emitted only when a parse
  // resolves — or immediately as `null` the moment the text is cleared to
  // empty.
  useEffect(() => {
    if (!draft.trim()) {
      // Empty query: emit null immediately (no debounce, no request) so the
      // filter clears the instant the field empties.
      setError(null);
      onChangeRef.current({ text: draft, condition: null, error: null });
      return;
    }
    const controller = new AbortController();
    const handle = setTimeout(() => {
      void runParse(draft, controller.signal);
    }, 250);
    return () => {
      controller.abort();
      clearTimeout(handle);
    };
  }, [draft, runParse]);

  // commit — the Enter / clear path. Forces an immediate parse (bypassing the
  // 250ms typing debounce) and, only when it comes back clean, notifies the
  // parent via onSubmit so it can persist the query (e.g. to the URL). An
  // invalid query keeps its inline error and is not committed. Reading the
  // live input value keeps this callback identity-stable across keystrokes.
  const commit = useCallback(() => {
    const text = inputRef.current?.value ?? "";
    const controller = new AbortController();
    void runParse(text, controller.signal).then((err) => {
      if (err === null) onSubmitRef.current?.(text);
    });
  }, [runParse]);

  // Syntax-highlight tokens for the overlay. Re-runs only when the draft
  // changes (cheap; the lexer is O(n) and called once per keystroke).
  const tokens = useMemo(() => tokenize(draft), [draft]);

  // Compute autocomplete suggestions whenever the draft or cursor moves.
  const suggestion = useMemo(() => {
    return suggest(draft, cursor, fields.data?.data ?? []);
    // We deliberately include `fields.data?.data` so the popover refreshes
    // after the catalog arrives. The reference is stable across queries
    // when the data hasn't changed (react-query gives identical refs).
  }, [draft, cursor, fields.data?.data]);

  // Keep the overlay scroll offset aligned with the input's. Without this,
  // typing past the visible width leaves the highlight visibly out of sync
  // with the cursor.
  useLayoutEffect(() => {
    const input = inputRef.current;
    const ov = overlayRef.current;
    if (!input || !ov) return;
    ov.scrollLeft = input.scrollLeft;
  }, [draft, cursor]);

  function syncCursor(): void {
    const el = inputRef.current;
    if (!el) return;
    setCursor(el.selectionStart ?? el.value.length);
  }

  function handleChange(e: React.ChangeEvent<HTMLInputElement>): void {
    // Update only the local draft — the parent is not notified per keystroke.
    // The debounce effect emits the parsed condition once it resolves.
    setDraft(e.target.value);
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
    const before = draft.slice(0, suggestion.replaceFrom);
    const after = draft.slice(suggestion.replaceTo);
    // Always include a trailing space after a field, operator or value so
    // typing continues naturally without a manual space.
    const inserted = s.value + (s.kind === "value" || s.kind === "keyword" ? " " : " ");
    const next = before + inserted + after;
    const newCursor = before.length + inserted.length;
    // Local draft only; the debounce effect re-parses the new text.
    setDraft(next);
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
    if (e.key === "Enter") {
      // With a suggestion highlighted, Enter accepts it (unchanged). Otherwise
      // — popover closed, or open with nothing to pick — Enter commits the
      // query to the parent (onSubmit), which is how the search lands in the
      // URL. Always preventDefault so a stray Enter never submits a form.
      e.preventDefault();
      const pick = open ? suggestion.items[activeIndex] : undefined;
      if (pick) {
        handleSelect(pick);
      } else {
        setOpen(false);
        cancelIdleClose();
        commit();
      }
      return;
    }
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
        nodes.push(draft.slice(pos, t.pos));
      }
      const cls = tokenClass(t);
      nodes.push(
        <span key={`${t.pos}-${t.kind}`} className={cls}>
          {draft.slice(t.pos, t.pos + t.len)}
        </span>,
      );
      pos = t.pos + t.len;
    }
    if (pos < draft.length) nodes.push(draft.slice(pos));
    return nodes;
  }, [tokens, draft]);

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
    // Emptying the draft triggers the debounce effect's immediate
    // null-condition emit, so we don't call onChange here directly.
    setDraft("");
    setOpen(false);
    // Clearing is a commit-to-empty: forward it so any persisted query (e.g.
    // the URL's ?search=) is dropped too, keeping the field and the URL aligned.
    onSubmitRef.current?.("");
    queueMicrotask(() => inputRef.current?.focus());
  }, []);

  return (
    <div
      className={[styles.wrap, error ? styles.invalid : null, className].filter(Boolean).join(" ")}
    >
      <span className={styles.leadIcon} aria-hidden="true">
        <Icon name="search" size={14} />
      </span>
      <div className={styles.inputBox}>
        <div ref={overlayRef} className={styles.overlay} aria-hidden="true">
          {draft.length > 0 ? (
            renderTokens()
          ) : (
            <span className={styles.placeholder}>{placeholder}</span>
          )}
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
          value={draft}
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
      {draft.length > 0 ? (
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
            {suggestion.kind === "field" &&
              (suggestion.field ? `Field for ${suggestion.field}` : "Field name")}
            {suggestion.kind === "operator" && "Operator"}
            {suggestion.kind === "value" &&
              (suggestion.field ? `Value for ${suggestion.field}` : "Value")}
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
