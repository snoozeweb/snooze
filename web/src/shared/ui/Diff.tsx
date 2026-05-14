// web/src/shared/ui/Diff.tsx
import { diffLines } from "diff";
import styles from "./Diff.module.css";

export type DiffProps = {
  oldText: string;
  newText: string;
};

export function Diff({ oldText, newText }: DiffProps) {
  if (oldText === newText) {
    return <div className={styles.empty}>No changes</div>;
  }
  const parts = diffLines(oldText, newText);
  const rows: { kind: "add" | "del" | "ctx"; line: string }[] = [];
  for (const p of parts) {
    const kind: "add" | "del" | "ctx" = p.added ? "add" : p.removed ? "del" : "ctx";
    const lines = p.value.replace(/\n$/, "").split("\n");
    for (const line of lines) rows.push({ kind, line });
  }
  return (
    <pre className={styles.pre} aria-label="Diff">
      {rows.map((r, i) => (
        <div key={i} className={styles[r.kind]}>
          {`${r.kind === "add" ? "+" : r.kind === "del" ? "-" : " "} ${r.line}`}
        </div>
      ))}
    </pre>
  );
}
