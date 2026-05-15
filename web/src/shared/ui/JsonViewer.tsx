import { useMemo, useState } from "react";
import type { ReactElement } from "react";
import { Icon } from "@/shared/icons/Icon";
import { IconButton } from "./IconButton";
import { toast } from "./toast/useToast";
import styles from "./JsonViewer.module.css";

export type JsonViewerProps = {
  value: unknown;
};

function renderPrimitive(v: unknown): ReactElement {
  if (v === null) return <span className={styles.null}>null</span>;
  if (typeof v === "string") return <span className={styles.string}>{JSON.stringify(v)}</span>;
  if (typeof v === "number") return <span className={styles.number}>{String(v)}</span>;
  if (typeof v === "boolean") return <span className={styles.boolean}>{String(v)}</span>;
  return <span>{JSON.stringify(v)}</span>;
}

function isObjectLike(v: unknown): v is Record<string, unknown> | unknown[] {
  return v !== null && typeof v === "object";
}

function indent(depth: number): string {
  return "  ".repeat(depth);
}

function renderValue(value: unknown, depth: number): ReactElement {
  if (!isObjectLike(value)) {
    return renderPrimitive(value);
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
            {renderValue(item, depth + 1)}
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
          <span className={styles.key}>{JSON.stringify(k)}</span>
          <span>: </span>
          {renderValue(v, depth + 1)}
          {i < entries.length - 1 ? "," : ""}
          {"\n"}
        </span>
      ))}
      {indent(depth)}
      <span>{"}"}</span>
    </>
  );
}

export function JsonViewer({ value }: JsonViewerProps) {
  const obj = useMemo<Record<string, unknown>>(
    () => (isObjectLike(value) && !Array.isArray(value) ? value : {}),
    [value],
  );
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set<string>());

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

  return (
    <div className={styles.wrap}>
      <div className={styles.toolbar}>
        <IconButton
          icon="copy"
          label="Copy JSON"
          size="sm"
          onClick={() => {
            void onCopy();
          }}
        />
      </div>
      <pre className={styles.pre}>
        {entries.length === 0 ? (
          <span>{"{}"}</span>
        ) : (
          <>
            <span>{"{"}</span>
            {"\n"}
            {entries.map(([k, v], i) => {
              const nested = isObjectLike(v);
              const isCollapsed = collapsed.has(k);
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
                  <span className={styles.key}>{JSON.stringify(k)}</span>
                  <span>: </span>
                  {nested && isCollapsed ? (
                    <span className={styles.muted}>
                      {Array.isArray(v)
                        ? `Array(${v.length})`
                        : `{ ${Object.keys(v).length} keys }`}
                    </span>
                  ) : (
                    renderValue(v, 1)
                  )}
                  {i < entries.length - 1 ? "," : ""}
                  {"\n"}
                </span>
              );
            })}
            <span>{"}"}</span>
          </>
        )}
      </pre>
    </div>
  );
}
