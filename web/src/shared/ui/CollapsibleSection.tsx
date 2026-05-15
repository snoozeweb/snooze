// CollapsibleSection — a section header that expands/collapses its body
// on click. Use to hide optional editor surfaces (time constraints,
// frequency, audit) behind a single line when they have no content, so
// the drawer fits the viewport without scrolling on the common path.
import { useState, type ReactNode } from "react";
import { Icon } from "@/shared/icons/Icon";
import styles from "./CollapsibleSection.module.css";

export type CollapsibleSectionProps = {
  title: string;
  /** Brief summary rendered to the right of the title when collapsed,
   *  e.g. "every day · 09:00–17:00". */
  summary?: ReactNode;
  /** Initial open state. */
  defaultOpen?: boolean;
  children: ReactNode;
};

export function CollapsibleSection({
  title,
  summary,
  defaultOpen = false,
  children,
}: CollapsibleSectionProps) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <section className={styles.section}>
      <button
        type="button"
        className={styles.header}
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
      >
        <span className={styles.title}>
          <span
            className={styles.chevron}
            style={{ transform: open ? "rotate(90deg)" : "none" }}
          >
            <Icon name="chevron-right" size={12} />
          </span>
          {title}
        </span>
        {summary !== undefined && !open ? (
          <span className={styles.summary}>{summary}</span>
        ) : null}
      </button>
      {open ? <div className={styles.body}>{children}</div> : null}
    </section>
  );
}
