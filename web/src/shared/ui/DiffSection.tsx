// web/src/shared/ui/DiffSection.tsx
import { useMemo, useState } from "react";
import { Diff } from "./Diff";
import { stableYaml } from "@/lib/yaml";
import styles from "./DiffSection.module.css";

export type DiffSectionProps = {
  original: unknown;
  current: unknown;
};

export function DiffSection({ original, current }: DiffSectionProps) {
  const [open, setOpen] = useState(false);
  // stableYaml deep-sorts then YAML-stringifies the whole object; only pay
  // for it while the section is expanded. Collapsed = no work at all, even
  // as `current` changes identity on every keystroke upstream.
  const oldText = useMemo(
    () => (open && original !== undefined ? stableYaml(original) : ""),
    [open, original],
  );
  const newText = useMemo(() => (open ? stableYaml(current) : ""), [open, current]);
  if (original === undefined) return null;
  return (
    <div className={styles.wrap}>
      <button
        type="button"
        className={styles.toggle}
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
      >
        Diff {open ? "▲" : "▼"}
      </button>
      {open ? (
        <div className={styles.body}>
          <Diff oldText={oldText} newText={newText} />
        </div>
      ) : null}
    </div>
  );
}
