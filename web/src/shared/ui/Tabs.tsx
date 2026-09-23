import * as RT from "@radix-ui/react-tabs";
import { useEffect, useRef, type ReactNode } from "react";
import styles from "./Tabs.module.css";

/** Slack before an edge counts as reached: sub-pixel scroll positions. */
const EDGE_SLACK_PX = 2;

/**
 * Marks which ends of a scrolling strip have more tabs beyond them, as
 * `data-fade-start` / `data-fade-end` on the list; the stylesheet turns each
 * into an edge fade.
 *
 * A strip that scrolls sideways on a phone clipped its last trigger mid-word
 * ("Re…") with nothing saying the row went on — no scrollbar on touch, no
 * hint. The fade is that hint. Written straight onto the node rather than
 * held in state: it changes on every scroll frame, and re-rendering the tab
 * strip (and through it whatever panel is open) per frame would be absurd for
 * a cosmetic mark. A strip that fits never gets either attribute, so the
 * desktop output is unchanged.
 */
function useScrollEdges() {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const update = () => {
      const overflow = el.scrollWidth - el.clientWidth;
      const start = overflow > EDGE_SLACK_PX && el.scrollLeft > EDGE_SLACK_PX;
      const end = overflow > EDGE_SLACK_PX && el.scrollLeft < overflow - EDGE_SLACK_PX;
      el.toggleAttribute("data-fade-start", start);
      el.toggleAttribute("data-fade-end", end);
    };
    update();
    el.addEventListener("scroll", update, { passive: true });
    // Width changes (rotation, the drawer resizing, a tab label growing a
    // count) move the edges without a scroll event.
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(update);
    observer?.observe(el);
    return () => {
      el.removeEventListener("scroll", update);
      observer?.disconnect();
    };
  }, []);
  return ref;
}

/**
 * Radix reports one click on a trigger twice: on mouse-down, and again on the
 * focus that mouse-down moves onto the trigger. Uncontrolled, the second call
 * is a no-op. Controlled from the URL, it is not — the route has not caught up
 * with the first navigation yet, so the second one pushes an identical history
 * entry and Back appears to do nothing. Both calls land in the same task, so
 * a value repeated before the task ends is dropped; a later click on the same
 * tab (after a switch was refused, say) still gets through.
 */
function useOnceOnValueChange(onValueChange: ((v: string) => void) | undefined) {
  const lastInTask = useRef<string | null>(null);
  if (onValueChange === undefined) return undefined;
  return (v: string) => {
    if (lastInTask.current === v) return;
    lastInTask.current = v;
    setTimeout(() => {
      lastInTask.current = null;
    }, 0);
    onValueChange(v);
  };
}

export function Tabs({
  defaultValue,
  value,
  onValueChange,
  children,
}: {
  defaultValue?: string;
  value?: string;
  onValueChange?: (v: string) => void;
  children: ReactNode;
}) {
  const handleValueChange = useOnceOnValueChange(onValueChange);
  return (
    <RT.Root
      {...(defaultValue !== undefined ? { defaultValue } : {})}
      {...(value !== undefined ? { value } : {})}
      {...(handleValueChange !== undefined ? { onValueChange: handleValueChange } : {})}
    >
      {children}
    </RT.Root>
  );
}

export function TabList({
  children,
  rightSlot,
  overflow = "wrap",
}: {
  children: ReactNode;
  /** Optional content rendered flush-right on the same row as the tab
   *  triggers. Used by list pages to put the bulk-action bar / "+ New"
   *  button next to the tab strip instead of stacking them vertically
   *  above the table. */
  rightSlot?: ReactNode;
  /**
   * What a strip does when it runs out of room.
   *
   * - `wrap` (default): triggers fall onto a second row. Right for a strip
   *   that spans the page, where a second row costs one line of header.
   * - `scroll`: the strip stays one row and scrolls sideways. For a strip
   *   boxed inside a narrow card, where the triggers want more width than the
   *   card has at every desktop size and wrapping would double the card's
   *   header instead of costing one line. Phones scroll either way.
   */
  overflow?: "wrap" | "scroll";
}) {
  const listClass = overflow === "scroll" ? `${styles.list} ${styles.scroll}` : styles.list;
  const listRef = useScrollEdges();
  if (rightSlot === undefined) {
    return (
      <RT.List ref={listRef} className={listClass}>
        {children}
      </RT.List>
    );
  }
  return (
    <div className={styles.headerRow}>
      <RT.List ref={listRef} className={listClass}>
        {children}
      </RT.List>
      <div className={styles.rightSlot}>{rightSlot}</div>
    </div>
  );
}

export function TabTrigger({
  value,
  children,
  disabled,
}: {
  value: string;
  children: ReactNode;
  disabled?: boolean;
}) {
  return (
    <RT.Trigger className={styles.trigger} value={value} disabled={disabled}>
      {children}
    </RT.Trigger>
  );
}

export function TabPanel({ value, children }: { value: string; children: ReactNode }) {
  return (
    <RT.Content className={styles.panel} value={value}>
      {children}
    </RT.Content>
  );
}
