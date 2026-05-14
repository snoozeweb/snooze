import { useEffect, useMemo, useRef, useState } from "react";
import * as RD from "@radix-ui/react-dialog";
import { useNavigate } from "@tanstack/react-router";
import { Icon } from "@/shared/icons/Icon";
import { Kbd } from "@/shared/ui/Kbd";
import { NAV_ITEMS, GROUP_LABELS, type NavGroup } from "./nav-items";
import styles from "./CommandPalette.module.css";

export type CommandPaletteProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

const GROUPS: NavGroup[] = ["operate", "configure", "admin"];

export function CommandPalette({ open, onOpenChange }: CommandPaletteProps) {
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement | null>(null);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return NAV_ITEMS;
    return NAV_ITEMS.filter((i) => i.label.toLowerCase().includes(q));
  }, [query]);

  useEffect(() => {
    setActiveIndex(0);
  }, [query]);

  useEffect(() => {
    if (open) {
      setQuery("");
      setActiveIndex(0);
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [open]);

  function select(i: number) {
    const item = filtered[i];
    if (!item) return;
    onOpenChange(false);
    void navigate({ to: item.to });
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActiveIndex((i) => Math.min(filtered.length - 1, i + 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActiveIndex((i) => Math.max(0, i - 1));
    } else if (e.key === "Enter") {
      e.preventDefault();
      select(activeIndex);
    }
  }

  const visibleGroups = GROUPS.filter((g) => filtered.some((i) => i.group === g));

  return (
    <RD.Root open={open} onOpenChange={onOpenChange}>
      <RD.Portal>
        <RD.Overlay className={styles.overlay} />
        <RD.Content className={styles.content} aria-label="Command palette">
          <RD.Title
            style={{
              position: "absolute",
              clip: "rect(0 0 0 0)",
              width: 1,
              height: 1,
              overflow: "hidden",
            }}
          >
            Command palette
          </RD.Title>
          <div className={styles.searchRow}>
            <span className={styles.searchIcon}>
              <Icon name="search" size={14} />
            </span>
            <input
              ref={inputRef}
              type="text"
              className={styles.search}
              placeholder="Jump to…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={onKeyDown}
            />
            <Kbd>Esc</Kbd>
          </div>
          {filtered.length === 0 ? (
            <div className={styles.empty}>No matches</div>
          ) : (
            <ul className={styles.list} role="listbox">
              {visibleGroups.map((group) => {
                const groupItems = filtered.filter((i) => i.group === group);
                return (
                  <li key={group}>
                    <div className={styles.group}>{GROUP_LABELS[group]}</div>
                    <ul style={{ listStyle: "none", padding: 0, margin: 0 }}>
                      {groupItems.map((item) => {
                        const globalIndex = filtered.indexOf(item);
                        return (
                          /* eslint-disable-next-line jsx-a11y/click-events-have-key-events */
                          <li
                            key={item.to}
                            role="option"
                            aria-selected={globalIndex === activeIndex}
                            className={styles.option}
                            onMouseEnter={() => setActiveIndex(globalIndex)}
                            onClick={() => select(globalIndex)}
                          >
                            <Icon name={item.icon} size={16} />
                            <span>{item.label}</span>
                            {item.shortcut ? (
                              <span className={styles.optionShortcut}>
                                {shortcutLabel(item.shortcut)}
                              </span>
                            ) : null}
                          </li>
                        );
                      })}
                    </ul>
                  </li>
                );
              })}
            </ul>
          )}
        </RD.Content>
      </RD.Portal>
    </RD.Root>
  );
}

function shortcutLabel(combo: string): string {
  return combo.replace(
    /mod/i,
    /mac/i.test(typeof navigator !== "undefined" ? navigator.platform : "") ? "⌘" : "Ctrl",
  );
}
