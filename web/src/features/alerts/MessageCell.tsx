import { useCallback, useRef, useState } from "react";
import { Tooltip } from "@/shared/ui/Tooltip";
import styles from "./MessageCell.module.css";

export type MessageCellProps = {
  /** The alert message. Callers render their own placeholder when it's empty. */
  text: string;
};

/**
 * MessageCell — the alert message, clamped to two lines.
 *
 * Two lines rather than one: at a normal desktop width that is ~140 characters
 * of what actually broke, which is where most alert messages end, so triage
 * stops costing a drawer-open per row. When a message still doesn't fit, the
 * full text arrives on hover — measured the same way DataTable's own
 * CellTooltip measures a horizontal ellipsis (compare scroll size to client
 * size on pointer entry), except the clamp overflows VERTICALLY, so the height
 * comparison is the one that matters. Short messages get no tooltip at all.
 */
export function MessageCell({ text }: MessageCellProps) {
  const ref = useRef<HTMLSpanElement>(null);
  const [clamped, setClamped] = useState(false);

  const checkClamped = useCallback(() => {
    const el = ref.current;
    if (!el) return;
    setClamped(el.scrollHeight > el.clientHeight + 1 || el.scrollWidth > el.clientWidth + 1);
  }, []);

  return (
    <Tooltip content={clamped ? text : null}>
      <span ref={ref} className={styles.message} onMouseEnter={checkClamped}>
        {text}
      </span>
    </Tooltip>
  );
}
