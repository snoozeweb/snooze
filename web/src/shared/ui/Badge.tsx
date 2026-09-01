import type { CSSProperties, ReactNode } from "react";
import styles from "./Badge.module.css";

export type BadgeVariant =
  | "neutral"
  | "muted"
  | "info"
  | "warning"
  | "error"
  | "critical"
  | "ok"
  | "ack"
  | "closed"
  | "admin"
  | "platform";

export type BadgeProps = {
  variant?: BadgeVariant;
  /**
   * Overrides `variant` with a concrete hex colour. Used by the gradated
   * per-severity alert badges so each severity tracks the dashboard palette
   * (lib/format/severity-color). Renders the colour as text + border on a
   * --badge-tint fill of the same colour; the variant class is dropped.
   */
  color?: string;
  children: ReactNode;
  className?: string;
  /**
   * Tooltip shown on hover. Defaults to the string children so a chip that
   * gets truncated by `.badge`'s own ellipsis (long notifier/integration
   * names etc.) still reads its full value on hover instead of clipping
   * silently. Pass `title=""` to opt out.
   */
  title?: string;
};

export function Badge({ variant = "neutral", color, children, className, title }: BadgeProps) {
  const classes = [styles.badge, color ? undefined : styles[variant], className]
    .filter(Boolean)
    .join(" ");
  // Same tint recipe as the variant classes, so a colour-prop badge and a
  // variant badge sit at the same weight — and so the fill follows the
  // themed --badge-tint instead of a fixed alpha that only worked in dark.
  const style: CSSProperties | undefined = color
    ? {
        color,
        background: `color-mix(in srgb, ${color} var(--badge-tint), transparent)`,
        border: `1px solid ${color}`,
      }
    : undefined;
  // `title=""` explicitly opts out (caller provides its own outer tooltip).
  // Distinguish "not provided" from "explicitly empty" with the `!==
  // undefined` check, then drop the attribute entirely when the resolved
  // value is falsy so an empty string never lands as `title=""` in the DOM.
  const resolvedTitle =
    title !== undefined ? title : typeof children === "string" ? children : undefined;
  return (
    <span className={classes} style={style} {...(resolvedTitle ? { title: resolvedTitle } : {})}>
      {children}
    </span>
  );
}
