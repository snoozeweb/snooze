// web/src/shared/ui/Diff.tsx
import { useMemo } from "react";
import { diffLines } from "diff";
import styles from "./Diff.module.css";

export type DiffProps = {
  oldText: string;
  newText: string;
};

type Row = { kind: "add" | "del" | "ctx"; line: string };

export function Diff({ oldText, newText }: DiffProps) {
  // diffLines is O(N*M); memoize so identical text on re-render is free.
  const rows = useMemo<Row[]>(() => {
    if (oldText === newText) return [];
    const out: Row[] = [];
    for (const p of diffLines(oldText, newText)) {
      const kind: Row["kind"] = p.added ? "add" : p.removed ? "del" : "ctx";
      const lines = p.value.replace(/\n$/, "").split("\n");
      for (const line of lines) out.push({ kind, line });
    }
    return out;
  }, [oldText, newText]);

  if (oldText === newText) {
    return <div className={styles.empty}>No changes</div>;
  }
  return (
    <pre className={styles.pre} aria-label="Diff">
      {rows.map((r, i) => (
        <div key={i} className={`${styles.row} ${styles[r.kind]}`}>
          {`${r.kind === "add" ? "+" : r.kind === "del" ? "-" : " "} ${r.line}`}
        </div>
      ))}
    </pre>
  );
}
