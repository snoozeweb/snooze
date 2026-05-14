import type { ReactNode } from "react";
import styles from "./Card.module.css";

export type CardProps = {
  elevated?: boolean;
  padded?: boolean;
  className?: string;
  children?: ReactNode;
};

export function Card({ elevated, padded, className, children }: CardProps) {
  const classes = [
    styles.card,
    elevated ? styles.elevated : null,
    padded ? styles.padded : null,
    className,
  ]
    .filter(Boolean)
    .join(" ");
  return <section className={classes}>{children}</section>;
}
