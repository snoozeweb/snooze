import type { ReactNode } from "react";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import styles from "./EmptyState.module.css";

export type EmptyStateProps = {
  icon?: IconName;
  title: string;
  description?: string;
  action?: ReactNode;
  className?: string;
};

export function EmptyState({ icon, title, description, action, className }: EmptyStateProps) {
  const classes = [styles.emptyState, className].filter(Boolean).join(" ");
  return (
    <div className={classes} role="status">
      {icon ? (
        <span className={styles.iconWrap}>
          <Icon name={icon} size={24} />
        </span>
      ) : null}
      <h3 className={styles.title}>{title}</h3>
      {description ? <p className={styles.description}>{description}</p> : null}
      {action ? <div className={styles.action}>{action}</div> : null}
    </div>
  );
}
