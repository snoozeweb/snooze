// MultiCombobox — pick zero or more options from a list. Selected entries
// render as removable badges (replaces the comma-separated text inputs the
// legacy Vue UI used for actions/roles/permissions). Wraps the same
// Radix Popover primitive that Combobox uses, so the styling stays
// consistent across the app.
import { useEffect, useId, useMemo, useRef, useState } from "react";
import * as RP from "@radix-ui/react-popover";
import { Icon } from "@/shared/icons/Icon";
import styles from "./MultiCombobox.module.css";

export type MultiComboboxOption = { value: string; label: string };

export type MultiComboboxProps = {
  options: MultiComboboxOption[];
  value: string[];
  onChange: (next: string[]) => void;
  placeholder?: string;
  noResultsLabel?: string;
  /** Allow values not present in `options` (free-form tags). */
  allowCustom?: boolean;
  className?: string;
  "aria-label"?: string;
};

export function MultiCombobox({
  options,
  value,
  onChange,
  placeholder = "Select…",
  noResultsLabel = "No results",
  allowCustom = false,
  className,
  "aria-label": ariaLabel,
}: MultiComboboxProps) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const contentRef = useRef<HTMLDivElement | null>(null);

  // When this popover opens inside a Drawer (Radix Dialog), the Dialog's
  // react-remove-scroll body-lock attaches a non-passive `wheel` listener
  // on `document` (bubble phase) and preventDefault()s every wheel event
  // that isn't on a whitelisted shard. Popover content portals outside
  // the Dialog, so it is not on the shard list. Radix Popover itself also
  // wraps content in its own RemoveScroll, adding a second layer.
  //
  // The robust fix: attach a CAPTURE-phase wheel listener at the document
  // level that, when the event originates inside this popover's content,
  // manually scrolls the nearest scrollable descendant and STOPS the
  // event entirely (stopImmediatePropagation + preventDefault). Capture
  // phase guarantees we run before any of the bubble-phase listeners
  // RemoveScroll attaches.
  useEffect(() => {
    if (!open) return;
    const popover = contentRef.current;
    if (!popover) return;
    const handler = (e: WheelEvent) => {
      const target = e.target as Node | null;
      if (!target || !popover.contains(target)) return;
      // Find the nearest scrollable element inside the popover. The list
      // is the obvious candidate but we walk up from the target so the
      // search-input area or any future scrollable inside also works.
      let el: HTMLElement | null = target as HTMLElement;
      while (el && el !== popover) {
        const style = window.getComputedStyle(el);
        const overflowY = style.overflowY;
        if ((overflowY === "auto" || overflowY === "scroll") && el.scrollHeight > el.clientHeight) {
          el.scrollTop += e.deltaY;
          e.preventDefault();
          e.stopImmediatePropagation();
          return;
        }
        el = el.parentElement;
      }
      // Nothing scrollable hit — still stop the event so the outer
      // body-lock listener doesn't preventDefault and freeze the page.
      e.stopImmediatePropagation();
    };
    document.addEventListener("wheel", handler, { capture: true, passive: false });
    return () => document.removeEventListener("wheel", handler, { capture: true });
  }, [open]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return options;
    return options.filter((o) => o.label.toLowerCase().includes(q));
  }, [options, query]);

  const isSelected = (v: string) => value.includes(v);

  function toggle(v: string) {
    if (isSelected(v)) {
      onChange(value.filter((x) => x !== v));
    } else {
      onChange([...value, v]);
    }
    setQuery("");
  }

  function remove(v: string) {
    onChange(value.filter((x) => x !== v));
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
      if (opt) {
        toggle(opt.value);
      } else if (allowCustom && query.trim()) {
        toggle(query.trim());
      }
    } else if (e.key === "Backspace" && query === "" && value.length > 0) {
      onChange(value.slice(0, -1));
    }
  }

  return (
    <RP.Root open={open} onOpenChange={setOpen}>
      <RP.Trigger asChild>
        <button
          type="button"
          className={[styles.wrap, className].filter(Boolean).join(" ")}
          role="combobox"
          aria-expanded={open}
          aria-controls={`${id}-listbox`}
          aria-label={ariaLabel}
        >
          {value.map((v) => {
            const opt = options.find((o) => o.value === v);
            return (
              <span key={v} className={styles.pill}>
                {opt?.label ?? v}
                <button
                  type="button"
                  aria-label={`Remove ${opt?.label ?? v}`}
                  onClick={(e) => {
                    // Don't propagate to the wrapping trigger button — clicking
                    // the X should only delete the pill, not also toggle the
                    // popover.
                    e.stopPropagation();
                    remove(v);
                  }}
                >
                  <Icon name="x" size={12} />
                </button>
              </span>
            );
          })}
          {value.length === 0 ? (
            <span className={styles.placeholder}>{placeholder}</span>
          ) : null}
          <span className={styles.caret} aria-hidden="true">
            <Icon name="chevron-down" size={14} />
          </span>
        </button>
      </RP.Trigger>
      <RP.Portal>
        <RP.Content
          ref={contentRef}
          className={styles.popContent}
          sideOffset={4}
          align="start"
          collisionPadding={8}
        >
          <div className={styles.searchWrap}>
            {/* eslint-disable jsx-a11y/no-autofocus -- popover search auto-focus pattern, matches Combobox.tsx */}
            <input
              type="text"
              autoFocus
              className={styles.search}
              value={query}
              placeholder={allowCustom ? "Search or type a new value…" : "Search"}
              onChange={(e) => {
                setQuery(e.target.value);
                setActiveIndex(0);
              }}
              onKeyDown={handleKeyDown}
            />
            {/* eslint-enable jsx-a11y/no-autofocus */}
          </div>
          {filtered.length === 0 ? (
            <div className={styles.empty}>
              {allowCustom && query.trim() ? `Press Enter to add "${query.trim()}"` : noResultsLabel}
            </div>
          ) : (
            <ul className={styles.list} role="listbox" id={`${id}-listbox`}>
              {filtered.map((opt, i) => (
                // eslint-disable-next-line jsx-a11y/click-events-have-key-events -- list-level keys live on the search input above (handleKeyDown delegates Enter to the active option).
                <li
                  key={opt.value}
                  role="option"
                  aria-selected={isSelected(opt.value)}
                  data-active={i === activeIndex || undefined}
                  className={styles.option}
                  onClick={() => toggle(opt.value)}
                  onMouseEnter={() => setActiveIndex(i)}
                >
                  <span>{opt.label}</span>
                  {isSelected(opt.value) ? (
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
