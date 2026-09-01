import { Icon } from "@/shared/icons/Icon";
import type { ErrorCopy } from "@/lib/api/errorMessage";
import styles from "./InlineError.module.css";

export type InlineErrorProps = ErrorCopy & {
  className?: string;
};

/**
 * A failure surfaced at the point of action — inside a dialog or drawer,
 * beside the control that failed — instead of only in a corner toast that
 * can be missed and disappears with the detail needed for a ticket.
 *
 * `summary` is the app's own sentence; `secondary` (when present) is the raw
 * server detail, rendered smaller/monospace so it reads as "the paste-into-a-
 * ticket part" rather than a second sentence of prose.
 */
export function InlineError({ summary, secondary, className }: InlineErrorProps) {
  const classes = [styles.error, className].filter(Boolean).join(" ");
  const iconClasses = [styles.icon].filter(Boolean).join(" ");
  return (
    <div className={classes} role="alert">
      <Icon name="alert-triangle" size={16} className={iconClasses} />
      <div className={styles.text}>
        <p className={styles.summary}>{summary}</p>
        {secondary && secondary !== summary ? (
          <p className={styles.secondary}>{secondary}</p>
        ) : null}
      </div>
    </div>
  );
}
