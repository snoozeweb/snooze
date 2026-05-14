import type { ReactNode } from "react";
import styles from "./Badge.module.css";

export type BadgeVariant = "neutral" | "muted" | "info" | "warning" | "error" | "critical" | "ok";

export type BadgeProps = {
  variant?: BadgeVariant;
  children: ReactNode;
  className?: string;
};

export function Badge({ variant = "neutral", children, className }: BadgeProps) {
  const classes = [styles.badge, styles[variant], className].filter(Boolean).join(" ");
  return <span className={classes}>{children}</span>;
}
