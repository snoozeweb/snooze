import * as RT from "@radix-ui/react-tabs";
import type { ReactNode } from "react";
import styles from "./Tabs.module.css";

export function Tabs({
  defaultValue,
  value,
  onValueChange,
  children,
}: {
  defaultValue?: string;
  value?: string;
  onValueChange?: (v: string) => void;
  children: ReactNode;
}) {
  return (
    <RT.Root
      {...(defaultValue !== undefined ? { defaultValue } : {})}
      {...(value !== undefined ? { value } : {})}
      {...(onValueChange !== undefined ? { onValueChange } : {})}
    >
      {children}
    </RT.Root>
  );
}

export function TabList({
  children,
  rightSlot,
}: {
  children: ReactNode;
  /** Optional content rendered flush-right on the same row as the tab
   *  triggers. Used by list pages to put the bulk-action bar / "+ New"
   *  button next to the tab strip instead of stacking them vertically
   *  above the table. */
  rightSlot?: ReactNode;
}) {
  if (rightSlot === undefined) {
    return <RT.List className={styles.list}>{children}</RT.List>;
  }
  return (
    <div className={styles.headerRow}>
      <RT.List className={styles.list}>{children}</RT.List>
      <div className={styles.rightSlot}>{rightSlot}</div>
    </div>
  );
}

export function TabTrigger({
  value,
  children,
  disabled,
}: {
  value: string;
  children: ReactNode;
  disabled?: boolean;
}) {
  return (
    <RT.Trigger className={styles.trigger} value={value} disabled={disabled}>
      {children}
    </RT.Trigger>
  );
}

export function TabPanel({ value, children }: { value: string; children: ReactNode }) {
  return (
    <RT.Content className={styles.panel} value={value}>
      {children}
    </RT.Content>
  );
}
