import { useEffect, useMemo, useRef, useState } from "react";
import type { KeyboardEvent, ReactElement, ReactNode } from "react";
import { Icon } from "@/shared/icons/Icon";
import { IconButton } from "./IconButton";
import { Input } from "./Input";
import { toast } from "./toast/useToast";
import styles from "./JsonViewer.module.css";

export type JsonViewerProps = {
  value: unknown;
  /** Show a find box over the tree: every match highlighted, Enter and
   *  Shift+Enter (or the arrows) step through them. */
  searchable?: boolean;
  /** Grow to the height the parent gives it and scroll inside, instead of
   *  capping at a fixed height. The parent must be a flex column with a
   *  bounded height. */
  fill?: boolean;
};

/**
 * Highlighter threads one search through a render: `mark` splits a token's
 * text around the matches and numbers each one in document order, so the
 * running count after the render is the total and `current` picks one.
 */
type Highlighter = {
  needle: string;
  current: number;
  count: number;
};

function mark(text: string, hl: Highlighter | null): ReactNode {
  if (!hl) return text;
  const hay = text.toLowerCase();
  // A lowercase that changes the length ("İ") would misplace every split
  // after it; such a token is shown unhighlighted rather than wrongly.
  if (hay.length !== text.length) return text;
  const parts: ReactNode[] = [];
  let from = 0;
  for (let at = hay.indexOf(hl.needle); at !== -1; at = hay.indexOf(hl.needle, from)) {
    if (at > from) parts.push(text.slice(from, at));
    const index = hl.count++;
    const isCurrent = index === hl.current;
    parts.push(
      <mark
        key={at}
        className={styles.match}
        data-match={index}
        {...(isCurrent ? { "data-current": "" } : {})}
      >
        {text.slice(at, at + hl.needle.length)}
      </mark>,
    );
    from = at + hl.needle.length;
  }
  if (parts.length === 0) return text;
  if (from < text.length) parts.push(text.slice(from));
  return parts;
}

function renderPrimitive(v: unknown, hl: Highlighter | null): ReactElement {
  if (v === null) return <span className={styles.null}>{mark("null", hl)}</span>;
  if (typeof v === "string")
    return <span className={styles.string}>{mark(JSON.stringify(v), hl)}</span>;
  if (typeof v === "number") return <span className={styles.number}>{mark(String(v), hl)}</span>;
  if (typeof v === "boolean") return <span className={styles.boolean}>{mark(String(v), hl)}</span>;
  return <span>{mark(JSON.stringify(v) ?? "", hl)}</span>;
}

function isObjectLike(v: unknown): v is Record<string, unknown> | unknown[] {
  return v !== null && typeof v === "object";
}

function indent(depth: number): string {
  return "  ".repeat(depth);
}

function renderValue(value: unknown, depth: number, hl: Highlighter | null): ReactElement {
  if (!isObjectLike(value)) {
    return renderPrimitive(value, hl);
  }
  if (Array.isArray(value)) {
    if (value.length === 0) return <span>[]</span>;
    return (
      <>
        <span>[</span>
        {"\n"}
        {value.map((item, i) => (
          <span key={i}>
            {indent(depth + 1)}
            {renderValue(item, depth + 1, hl)}
            {i < value.length - 1 ? "," : ""}
            {"\n"}
          </span>
        ))}
        {indent(depth)}
        <span>]</span>
      </>
    );
  }
  const entries = Object.entries(value);
  if (entries.length === 0) return <span>{"{}"}</span>;
  return (
    <>
      <span>{"{"}</span>
      {"\n"}
      {entries.map(([k, v], i) => (
        <span key={k}>
          {indent(depth + 1)}
          <span className={styles.key}>{mark(JSON.stringify(k), hl)}</span>
          <span>: </span>
          {renderValue(v, depth + 1, hl)}
          {i < entries.length - 1 ? "," : ""}
          {"\n"}
        </span>
      ))}
      {indent(depth)}
      <span>{"}"}</span>
    </>
  );
}

export function JsonViewer({ value, searchable = false, fill = false }: JsonViewerProps) {
  const obj = useMemo<Record<string, unknown>>(
    () => (isObjectLike(value) && !Array.isArray(value) ? value : {}),
    [value],
  );
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set<string>());
  const [query, setQuery] = useState("");
  const [current, setCurrent] = useState(0);
  const preRef = useRef<HTMLPreElement>(null);

  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(JSON.stringify(value, null, 2));
      toast.success("Copied JSON to clipboard");
    } catch {
      toast.error("Copy failed");
    }
  };

  const toggle = (key: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  const entries = Object.entries(obj);
  const needle = searchable ? query.trim().toLowerCase() : "";
  // A search looks through everything: a match hidden in a collapsed key
  // would be counted but impossible to show.
  const searching = needle !== "";
  const hl: Highlighter | null = searching ? { needle, current, count: 0 } : null;

  // Built before the return so the highlighter's count — the total — is known
  // when the counter below renders.
  const tree =
    entries.length === 0 ? (
      <span>{"{}"}</span>
    ) : (
      <>
        <span>{"{"}</span>
        {"\n"}
        {entries.map(([k, v], i) => {
          const nested = isObjectLike(v);
          const isCollapsed = !searching && collapsed.has(k);
          return (
            <span key={k}>
              {"  "}
              {nested ? (
                <button
                  type="button"
                  className={styles.chevron}
                  aria-label={`Toggle ${k}`}
                  aria-expanded={!isCollapsed}
                  onClick={() => toggle(k)}
                >
                  <Icon name={isCollapsed ? "chevron-right" : "chevron-down"} size={12} />
                </button>
              ) : (
                <span className={styles.chevronPlaceholder} aria-hidden="true" />
              )}
              <span className={styles.key}>{mark(JSON.stringify(k), hl)}</span>
              <span>: </span>
              {nested && isCollapsed ? (
                <span className={styles.muted}>
                  {Array.isArray(v) ? `Array(${v.length})` : `{ ${Object.keys(v).length} keys }`}
                </span>
              ) : (
                renderValue(v, 1, hl)
              )}
              {i < entries.length - 1 ? "," : ""}
              {"\n"}
            </span>
          );
        })}
        <span>{"}"}</span>
      </>
    );
  const total = hl?.count ?? 0;
  const active = total === 0 ? 0 : Math.min(current, total - 1);

  // Bring the current match into view — inside the <pre> only, never the
  // page: `block: "nearest"` leaves it alone when the match is already shown.
  useEffect(() => {
    if (!searching || total === 0) return;
    const el = preRef.current?.querySelector<HTMLElement>(`mark[data-match="${active}"]`);
    el?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
  }, [searching, needle, active, total]);

  const step = (delta: number) => {
    if (total === 0) return;
    setCurrent((active + delta + total) % total);
  };

  const onSearchKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== "Enter") return;
    e.preventDefault();
    step(e.shiftKey ? -1 : 1);
  };

  const copyButton = (
    <IconButton
      icon="copy"
      label="Copy JSON"
      size="sm"
      onClick={() => {
        void onCopy();
      }}
    />
  );

  return (
    <div className={styles.wrap} data-fill={fill || undefined}>
      {searchable ? (
        <div className={styles.searchBar}>
          <Input
            type="search"
            size="sm"
            leadingIcon="search"
            className={styles.searchField}
            placeholder="Find in record"
            aria-label="Find in record"
            autoComplete="off"
            spellCheck={false}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setCurrent(0);
            }}
            onKeyDown={onSearchKeyDown}
          />
          {/* Always mounted so the live region announces the first count. */}
          <span role="status" className={styles.searchCount}>
            {searching ? (total === 0 ? "No matches" : `${active + 1} of ${total}`) : null}
          </span>
          <IconButton
            icon="chevron-up"
            label="Previous match (Shift+Enter)"
            size="sm"
            disabled={total === 0}
            onClick={() => step(-1)}
          />
          <IconButton
            icon="chevron-down"
            label="Next match (Enter)"
            size="sm"
            disabled={total === 0}
            onClick={() => step(1)}
          />
          {copyButton}
        </div>
      ) : (
        <div className={styles.toolbar}>{copyButton}</div>
      )}
      <pre ref={preRef} className={styles.pre} data-searchable={searchable || undefined}>
        {tree}
      </pre>
    </div>
  );
}
