import type { ReactNode } from "react";
import type React from "react";
import * as RM from "@radix-ui/react-dropdown-menu";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import styles from "./Menu.module.css";

export const Menu = RM.Root;

export type MenuTriggerProps = RM.DropdownMenuTriggerProps & {
  ref?: React.Ref<HTMLButtonElement>;
};

export function MenuTrigger({ ref, ...props }: MenuTriggerProps) {
  // asChild lets the consumer (typically <IconButton>) be the trigger
  // rather than wrapping it in Radix's default <button>, which inherits
  // the platform's chrome (white box in light mode, dark in dark mode).
  return <RM.Trigger asChild {...props} ref={ref} />;
}

export function MenuContent({
  children,
  side = "bottom",
  align = "end",
}: {
  children: ReactNode;
  side?: "top" | "right" | "bottom" | "left";
  align?: "start" | "center" | "end";
}) {
  return (
    <RM.Portal>
      <RM.Content className={styles.content} side={side} align={align} sideOffset={4}>
        {children}
      </RM.Content>
    </RM.Portal>
  );
}

export type MenuItemProps = {
  onSelect?: () => void;
  disabled?: boolean;
  danger?: boolean;
  leadingIcon?: IconName;
  shortcut?: string;
  /** One plain sentence under the label explaining what the verb actually
   *  does. Used where a menu gathers several near-synonyms an operator has
   *  to choose between (Close / Snooze / Shelve / Shelve permanently) and
   *  the label alone doesn't say which one to pick. Rendered as real text
   *  inside the item, so assistive tech reads it with the item rather than
   *  needing a hover; `textValue` below keeps typeahead on the label. */
  description?: string;
  children: ReactNode;
};

export function MenuItem({
  onSelect,
  disabled,
  danger,
  leadingIcon,
  shortcut,
  description,
  children,
}: MenuItemProps) {
  const classes = [
    styles.item,
    danger ? styles.danger : null,
    description ? styles.withDescription : null,
  ]
    .filter(Boolean)
    .join(" ");
  return (
    <RM.Item
      className={classes}
      {...(disabled !== undefined ? { disabled } : {})}
      {...(onSelect !== undefined ? { onSelect } : {})}
      {...(description && typeof children === "string" ? { textValue: children } : {})}
    >
      {leadingIcon ? <Icon name={leadingIcon} size={16} /> : null}
      <span className={styles.itemText}>
        <span>{children}</span>
        {description ? <span className={styles.itemDescription}>{description}</span> : null}
      </span>
      {shortcut ? <span className={styles.shortcut}>{shortcut}</span> : null}
    </RM.Item>
  );
}

/** A non-interactive heading above a group of items. Radix skips it in
 *  keyboard navigation and exposes it as the group's label, so it names a
 *  cluster ("Quiet it down") without adding a stop to arrow-key traversal. */
export function MenuLabel({ children }: { children: ReactNode }) {
  return <RM.Label className={styles.groupLabel}>{children}</RM.Label>;
}

export function MenuSeparator() {
  return <RM.Separator className={styles.separator} />;
}
