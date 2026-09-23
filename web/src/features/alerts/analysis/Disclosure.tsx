// A section of the Analysis tab that starts folded: its heading IS the toggle.
//
// Used for the parts of an analysis that support the verdict but are not read
// first — the evidence, the rollback. The heading carries the count ("Evidence
// · 7") so a folded section still says how much is behind it; the WAI-ARIA
// disclosure pattern (a button inside the heading, `aria-expanded`) keeps it a
// heading for screen-reader navigation and a button for everyone else.
//
// Deliberately not shared/ui/CollapsibleSection: that one is an editor-form
// affordance with an uppercase caption for a title, and these are content
// headings that have to sit in the same type scale as "What to do".
import { useId, useState, type ReactNode } from "react";
import { Icon } from "@/shared/icons/Icon";
import styles from "./Disclosure.module.css";

export type DisclosureProps = {
  /** The heading text, count included ("Evidence · 7"). */
  label: string;
  /** Section headings are h3 in this pane; a sub-part of one is h4. */
  level?: 3 | 4;
  defaultOpen?: boolean;
  children: ReactNode;
};

export function Disclosure({ label, level = 3, defaultOpen = false, children }: DisclosureProps) {
  const [open, setOpen] = useState(defaultOpen);
  const bodyId = useId();
  const Heading = level === 3 ? "h3" : "h4";
  return (
    <section className={styles.disclosure}>
      <Heading className={styles.heading} data-level={level}>
        <button
          type="button"
          className={styles.toggle}
          aria-expanded={open}
          {...(open ? { "aria-controls": bodyId } : {})}
          onClick={() => setOpen((o) => !o)}
        >
          <span className={styles.chevron} data-open={open ? "true" : undefined}>
            <Icon name="chevron-right" size={14} />
          </span>
          {label}
        </button>
      </Heading>
      {open ? (
        <div id={bodyId} className={styles.body}>
          {children}
        </div>
      ) : null}
    </section>
  );
}
