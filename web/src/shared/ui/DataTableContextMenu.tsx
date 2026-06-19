import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { copyToClipboard } from "@/lib/clipboard";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import { toast } from "@/shared/ui/toast/useToast";
import styles from "./DataTableContextMenu.module.css";

export type ContextMenuItem = {
  key: string;
  label: string;
  icon?: IconName;
  danger?: boolean;
  disabled?: boolean;
  onSelect: () => void | Promise<void>;
};

export type DataTableContextMenuProps = {
  items: ContextMenuItem[];
  x: number;
  y: number;
  onClose: () => void;
  /**
   * Highlighted text captured when the menu opened. When non-blank, a "Copy"
   * item is prepended to the top of the menu that copies exactly this text —
   * so a user who selected text inside a row can grab it (the row-open click
   * is suppressed during a selection; see DataTable.handleRowClick). Blank or
   * omitted → no Copy item, and the menu renders the consumer's items as-is.
   */
  copyText?: string;
};

export function DataTableContextMenu({
  items,
  x,
  y,
  onClose,
  copyText,
}: DataTableContextMenuProps) {
  const ref = useRef<HTMLUListElement>(null);
  const [focused, setFocused] = useState<number>(0);
  const [pos, setPos] = useState<{ left: number; top: number }>({ left: x, top: y });

  // Prepend a "Copy" item when text is selected. Memoized on (items, copyText)
  // so its identity stays stable across the internal re-renders driven by
  // focus changes — the focus-reset and keydown effects key off this array and
  // would otherwise reset the highlight on every arrow press.
  const menuItems = useMemo<ContextMenuItem[]>(() => {
    if (copyText && copyText.trim() !== "") {
      const copyItem: ContextMenuItem = {
        key: "__copy-selection",
        label: "Copy",
        icon: "copy",
        onSelect: async () => {
          const ok = await copyToClipboard(copyText);
          if (ok) toast.success("Copied to clipboard");
          else toast.error("Clipboard unavailable");
        },
      };
      return [copyItem, ...items];
    }
    return items;
  }, [items, copyText]);

  // Adjust position so the menu stays within the viewport.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    let left = x;
    let top = y;
    if (left + rect.width > vw) left = Math.max(0, vw - rect.width - 4);
    if (top + rect.height > vh) top = Math.max(0, vh - rect.height - 4);
    setPos({ left, top });
  }, [x, y]);

  useEffect(() => {
    function onDocClick(e: MouseEvent) {
      if (!ref.current) return;
      if (!ref.current.contains(e.target as Node)) onClose();
    }
    function onScroll() {
      onClose();
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        e.preventDefault();
        onClose();
      } else if (e.key === "ArrowDown") {
        e.preventDefault();
        setFocused((i) => {
          const next = menuItems.findIndex((it, idx) => idx > i && !it.disabled);
          if (next !== -1) return next;
          const first = menuItems.findIndex((it) => !it.disabled);
          return first === -1 ? i : first;
        });
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setFocused((i) => {
          for (let k = i - 1; k >= 0; k--) {
            const it = menuItems[k];
            if (it && !it.disabled) return k;
          }
          for (let k = menuItems.length - 1; k > i; k--) {
            const it = menuItems[k];
            if (it && !it.disabled) return k;
          }
          return i;
        });
      } else if (e.key === "Enter") {
        const item = menuItems[focused];
        if (item && !item.disabled) {
          e.preventDefault();
          onClose();
          void item.onSelect();
        }
      }
    }
    document.addEventListener("mousedown", onDocClick, true);
    document.addEventListener("contextmenu", onDocClick, true);
    document.addEventListener("scroll", onScroll, true);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDocClick, true);
      document.removeEventListener("contextmenu", onDocClick, true);
      document.removeEventListener("scroll", onScroll, true);
      document.removeEventListener("keydown", onKey);
    };
  }, [menuItems, focused, onClose]);

  useEffect(() => {
    const first = menuItems.findIndex((it) => !it.disabled);
    if (first !== -1) setFocused(first);
  }, [menuItems]);

  // Focus the menu container on mount so keyboard events are captured
  // immediately. The document-level keydown listener handles Arrow/Enter/Escape
  // regardless of which element has focus, but focusing the container ensures
  // AT announces the menu and gives a sensible return target on close.
  useEffect(() => {
    ref.current?.focus({ preventScroll: true });
  }, []);

  const menu = (
    <ul
      ref={ref}
      role="menu"
      aria-label="Row context menu"
      tabIndex={-1}
      className={styles.menu}
      style={{ left: pos.left, top: pos.top }}
    >
      {menuItems.map((item, idx) => {
        const classes = [
          styles.item,
          item.danger ? styles.danger : null,
          idx === focused ? styles.focused : null,
          item.disabled ? styles.disabled : null,
        ]
          .filter(Boolean)
          .join(" ");
        return (
          <li
            key={item.key}
            role="menuitem"
            aria-disabled={item.disabled ? true : undefined}
            tabIndex={-1}
            className={classes}
            onMouseEnter={() => setFocused(idx)}
            onClick={(e) => {
              e.stopPropagation();
              if (item.disabled) return;
              onClose();
              void item.onSelect();
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                if (item.disabled) return;
                onClose();
                void item.onSelect();
              }
            }}
          >
            {item.icon ? <Icon name={item.icon} size={16} /> : <span className={styles.iconSlot} />}
            <span>{item.label}</span>
          </li>
        );
      })}
    </ul>
  );

  return createPortal(menu, document.body);
}
