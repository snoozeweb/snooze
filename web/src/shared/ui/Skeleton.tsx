import type { CSSProperties } from "react";
import styles from "./Skeleton.module.css";

export type SkeletonProps = {
  width?: string | number;
  height?: string | number;
  radius?: "sm" | "md" | "lg" | "pill";
  className?: string;
};

export function Skeleton({
  width = "100%",
  height = "1em",
  radius = "md",
  className,
}: SkeletonProps) {
  const style: CSSProperties = {
    width: typeof width === "number" ? `${width}px` : width,
    height: typeof height === "number" ? `${height}px` : height,
  };
  const classes = [styles.skeleton, styles[radius], className].filter(Boolean).join(" ");
  return <span className={classes} style={style} aria-hidden="true" data-testid="skeleton" />;
}
