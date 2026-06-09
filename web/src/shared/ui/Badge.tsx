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
  | "closed"
  | "platform";

export type BadgeProps = {
  variant?: BadgeVariant;
  /**
   * Overrides `variant` with a concrete hex colour. Used by the gradated
   * per-severity alert badges so each severity tracks the dashboard palette
   * (lib/format/severity-color). Renders the colour as text + border on a
   * 15%-alpha tint of the same colour; the variant class is dropped.
   */
  color?: string;
  children: ReactNode;
  className?: string;
};

export function Badge({ variant = "neutral", color, children, className }: BadgeProps) {
  const classes = [styles.badge, color ? undefined : styles[variant], className]
    .filter(Boolean)
    .join(" ");
  // `${color}26` appends an alpha byte (0x26 ≈ 15%) to a #rrggbb hex.
  const style: CSSProperties | undefined = color
    ? { color, background: `${color}26`, border: `1px solid ${color}` }
    : undefined;
  return (
    <span className={classes} style={style}>
      {children}
    </span>
  );
}
