import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
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
};

export function DataTableContextMenu({ items, x, y, onClose }: DataTableContextMenuProps) {
  const ref = useRef<HTMLUListElement>(null);
  const [focused, setFocused] = useState<number>(0);
  const [pos, setPos] = useState<{ left: number; top: number }>({ left: x, top: y });

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
          const next = items.findIndex((it, idx) => idx > i && !it.disabled);
          if (next !== -1) return next;
          const first = items.findIndex((it) => !it.disabled);
          return first === -1 ? i : first;
        });
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setFocused((i) => {
          for (let k = i - 1; k >= 0; k--) {
            const it = items[k];
            if (it && !it.disabled) return k;
          }
          for (let k = items.length - 1; k > i; k--) {
            const it = items[k];
            if (it && !it.disabled) return k;
          }
          return i;
        });
      } else if (e.key === "Enter") {
        const item = items[focused];
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
  }, [items, focused, onClose]);

  useEffect(() => {
    const first = items.findIndex((it) => !it.disabled);
    if (first !== -1) setFocused(first);
  }, [items]);

  const menu = (
    <ul
      ref={ref}
      role="menu"
      aria-label="Row context menu"
      className={styles.menu}
      style={{ left: pos.left, top: pos.top }}
    >
      {items.map((item, idx) => {
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
