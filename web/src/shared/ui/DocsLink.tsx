import type { ReactNode } from "react";
import { Icon } from "@/shared/icons/Icon";
import { docsUrl } from "@/lib/docs";
import styles from "./DocsLink.module.css";

/**
 * DocsLink — a small, consistent "Learn more ↗" affordance that deep-links into
 * the published documentation (snoozeweb.github.io/snooze) from anywhere in the
 * SPA. Standardises the inline `<a href={docsUrl(slug)} target="_blank">` +
 * book-icon pattern so every self-explaining concept link looks and behaves the
 * same.
 */
export function DocsLink({
  slug,
  children = "Learn more",
  className,
}: {
  /** Route slug under the docs site, e.g. "general/rules". */
  slug: string;
  children?: ReactNode;
  className?: string;
}) {
  return (
    <a
      className={[styles.docsLink, className].filter(Boolean).join(" ")}
      href={docsUrl(slug)}
      target="_blank"
      rel="noreferrer"
    >
      <Icon name="book" size={14} />
      {children}
      <span aria-hidden="true">↗</span>
    </a>
  );
}
