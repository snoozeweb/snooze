import { forwardRef } from "react";
import type { ReactNode } from "react";
import * as RM from "@radix-ui/react-dropdown-menu";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import styles from "./Menu.module.css";

export const Menu = RM.Root;

export const MenuTrigger = forwardRef<HTMLButtonElement, RM.DropdownMenuTriggerProps>(
  function MenuTrigger(props, ref) {
    // asChild lets the consumer (typically <IconButton>) be the trigger
    // rather than wrapping it in Radix's default <button>, which inherits
    // the platform's chrome (white box in light mode, dark in dark mode).
    return <RM.Trigger asChild {...props} ref={ref} />;
  },
);

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
  children: ReactNode;
};

export function MenuItem({
  onSelect,
  disabled,
  danger,
  leadingIcon,
  shortcut,
  children,
}: MenuItemProps) {
  const classes = [styles.item, danger ? styles.danger : null].filter(Boolean).join(" ");
  return (
    <RM.Item
      className={classes}
      {...(disabled !== undefined ? { disabled } : {})}
      {...(onSelect !== undefined ? { onSelect } : {})}
    >
      {leadingIcon ? <Icon name={leadingIcon} size={16} /> : null}
      <span>{children}</span>
      {shortcut ? <span className={styles.shortcut}>{shortcut}</span> : null}
    </RM.Item>
  );
}

export function MenuSeparator() {
  return <RM.Separator className={styles.separator} />;
}
