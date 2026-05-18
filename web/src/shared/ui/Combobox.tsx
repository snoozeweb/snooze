import { useMemo, useState } from "react";
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
