import type { ReactNode } from "react";
import styles from "./Kbd.module.css";

export type KbdProps = { children: ReactNode; className?: string };

export function Kbd({ children, className }: KbdProps) {
  const classes = [styles.kbd, className].filter(Boolean).join(" ");
  return <kbd className={classes}>{children}</kbd>;
}
