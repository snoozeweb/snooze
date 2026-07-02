import type { ReactNode } from "react";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import { DocsLink } from "./DocsLink";
import styles from "./EmptyState.module.css";

export type EmptyStateProps = {
  icon?: IconName;
  title: string;
  description?: string;
  action?: ReactNode;
  /** When set, renders a "Learn more ↗" docs link (below the description) that
   *  deep-links into the documentation — the empty state is often a
   *  first-time user's only chance to learn what a concept is for. */
  docsSlug?: string;
  className?: string;
};

export function EmptyState({
  icon,
  title,
  description,
  action,
  docsSlug,
  className,
}: EmptyStateProps) {
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
      {docsSlug ? <DocsLink slug={docsSlug} /> : null}
      {action ? <div className={styles.action}>{action}</div> : null}
    </div>
  );
}
