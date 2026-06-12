import { useEffect, useMemo, useRef, useState } from "react";
import * as RP from "@radix-ui/react-popover";
import { Icon } from "@/shared/icons/Icon";
import styles from "./Combobox.module.css";

export type ComboboxOption = { value: string; label: string };

export type ComboboxProps = {
  options: ComboboxOption[];
  value?: string;
  onValueChange: (value: string) => void;
  placeholder?: string;
  noResultsLabel?: string;
  className?: string;
};

export function Combobox({
  options,
  value,
  onValueChange,
  placeholder = "Select…",
  noResultsLabel = "No results",
  className,
}: ComboboxProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const contentRef = useRef<HTMLDivElement | null>(null);

  // See MultiCombobox.tsx for the full explanation. Window-capture wheel
  // listener fires before any RemoveScroll listener anywhere — when the
  // event targets this popover, scroll manually and stop the event.
  // Gated on `open` so we only hold a non-passive capture listener while
  // the popover is actually visible; effects run post-commit so the portal
  // content ref is already populated when the effect attaches.
  useEffect(() => {
    if (!open) return;
    const handler = (e: WheelEvent) => {
      const popover = contentRef.current;
      if (!popover) return;
      const target = e.target as Node | null;
      if (!target || !popover.contains(target)) return;
      let el: HTMLElement | null = target as HTMLElement;
      while (el && el !== popover) {
        const styles = window.getComputedStyle(el);
        const overflowY = styles.overflowY;
        if ((overflowY === "auto" || overflowY === "scroll") && el.scrollHeight > el.clientHeight) {
          el.scrollTop += e.deltaY;
          e.preventDefault();
          e.stopImmediatePropagation();
          return;
        }
        el = el.parentElement;
      }
      e.stopImmediatePropagation();
    };
    window.addEventListener("wheel", handler, { capture: true, passive: false });
    return () => window.removeEventListener("wheel", handler, { capture: true });
  }, [open]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return options;
    return options.filter((o) => o.label.toLowerCase().includes(q));
  }, [options, query]);

  const selected = options.find((o) => o.value === value);

  function selectOption(opt: ComboboxOption) {
    onValueChange(opt.value);
    setOpen(false);
    setQuery("");
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActiveIndex((i) => Math.min(filtered.length - 1, i + 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActiveIndex((i) => Math.max(0, i - 1));
    } else if (e.key === "Enter") {
      e.preventDefault();
      const opt = filtered[activeIndex];
      if (opt) selectOption(opt);
    }
  }

  return (
    <RP.Root open={open} onOpenChange={setOpen}>
      <RP.Trigger
        role="combobox"
        aria-expanded={open}
        className={[styles.trigger, className].filter(Boolean).join(" ")}
      >
        <span className={selected ? "" : styles.placeholder}>
          {selected ? selected.label : placeholder}
        </span>
        <Icon name="chevron-down" size={14} />
      </RP.Trigger>
      <RP.Portal>
        <RP.Content
          ref={contentRef}
          className={styles.content}
          sideOffset={4}
          align="start"
          collisionPadding={8}
        >
          <div className={styles.searchWrap}>
            {/* eslint-disable jsx-a11y/no-autofocus -- Search field in a popover should grab focus immediately so users can type without an extra click. */}
            <input
              type="text"
              autoFocus
              className={styles.search}
              value={query}
              placeholder="Search"
              onChange={(e) => {
                setQuery(e.target.value);
                setActiveIndex(0);
              }}
              onKeyDown={handleKeyDown}
            />
            {/* eslint-enable jsx-a11y/no-autofocus */}
          </div>
          {filtered.length === 0 ? (
            <div className={styles.empty}>{noResultsLabel}</div>
          ) : (
            <ul className={styles.list} role="listbox">
              {filtered.map((opt, i) => (
                // eslint-disable-next-line jsx-a11y/click-events-have-key-events -- Keyboard input is delegated to the search field above the list (which captures ArrowUp/Down/Enter and routes selection through selectOption), so the listbox items themselves don't need their own keyboard handlers.
                <li
                  key={opt.value}
                  role="option"
                  aria-selected={i === activeIndex}
                  className={styles.option}
                  onClick={() => selectOption(opt)}
                  onMouseEnter={() => setActiveIndex(i)}
                >
                  <span>{opt.label}</span>
                  {opt.value === value ? (
                    <span className={styles.optionCheck}>
                      <Icon name="check" size={12} />
                    </span>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </RP.Content>
      </RP.Portal>
    </RP.Root>
  );
}
