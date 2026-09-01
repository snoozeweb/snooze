import { useEffect, useMemo, useRef, useState } from "react";
import * as RD from "@radix-ui/react-dialog";
import { useNavigate } from "@tanstack/react-router";
import { Icon } from "@/shared/icons/Icon";
import { Kbd } from "@/shared/ui/Kbd";
import { Records } from "@/features/alerts/api";
import type { Record_ } from "@/features/alerts/types";
import { encodeConditionQ } from "@/lib/condition/serialize";
import type { Condition } from "@/lib/condition/types";
import { usePaletteActions, type PaletteAction } from "@/shared/hooks/usePaletteActions";
import { NAV_ITEMS, GROUP_LABELS, type NavGroup, type NavItem } from "./nav-items";
import styles from "./CommandPalette.module.css";

export type CommandPaletteProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

const GROUPS: NavGroup[] = ["operate", "configure", "admin"];

const LISTBOX_ID = "command-palette-listbox";

/** Shortest query that is worth a round trip to the record collection. */
const ALERT_SEARCH_MIN = 2;
/** The palette is a jump list, not a results page — a handful, then go look. */
const ALERT_SEARCH_LIMIT = 6;
const ALERT_SEARCH_DEBOUNCE_MS = 200;

/**
 * A palette row. Navigation entries come first and are never displaced by a
 * search — jumping between pages is what an operator reaches for ⌘K to do, and
 * a slow network must not change which row Enter lands on.
 */
type Entry =
  | { kind: "nav"; item: NavItem }
  | { kind: "action"; action: PaletteAction }
  | { kind: "alert"; record: Record_ };

// Stable per-option DOM id. `to` values like "/web/alerts" are sanitized to
// "cmdpalette-opt-web-alerts" so they can serve as aria-activedescendant
// targets; alert/action rows are keyed the same way off their own identity.
function optionId(raw: string): string {
  return `cmdpalette-opt-${raw.replace(/[^a-zA-Z0-9]+/g, "-").replace(/^-|-$/g, "")}`;
}

function entryId(e: Entry): string {
  if (e.kind === "nav") return optionId(e.item.to);
  if (e.kind === "action") return optionId(`action-${e.action.id}`);
  return optionId(`alert-${recordId(e.record)}`);
}

function recordId(r: Record_): string {
  return r.uid ?? `${r.host ?? ""}-${r.date_epoch ?? 0}`;
}

/**
 * The same condition the SearchBar produces for bare text (see
 * lib/condition/text.ts: a value with no operator lowers to SEARCH), so typing
 * "disk" here and typing "disk" into the alerts search mean the same thing and
 * return the same rows. SEARCH covers message and host — the two fields an
 * operator recalls an alert by — along with the rest of the record.
 *
 * Deliberately NOT `CONTAINS message` OR `CONTAINS host`: the SQLite driver's
 * CONTAINS feeds the field through json_each to handle array-valued fields,
 * which raises "malformed JSON" on a plain text field (see the note in the
 * hand-off — a pre-existing backend bug, reachable today from the SearchBar's
 * own CONTAINS operator).
 */
function alertSearchCondition(q: string): Condition {
  return { type: "SEARCH", field: "", value: q };
}

export function CommandPalette({ open, onOpenChange }: CommandPaletteProps) {
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const pageActions = usePaletteActions();

  // Debounced copy of the query — the alert search is a server round trip, so
  // it lags the input while the nav/action filtering above stays instant.
  const [debouncedQuery, setDebouncedQuery] = useState("");
  useEffect(() => {
    const t = setTimeout(() => setDebouncedQuery(query.trim()), ALERT_SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(t);
  }, [query]);

  const alertQ = useMemo(
    () =>
      debouncedQuery.length >= ALERT_SEARCH_MIN
        ? encodeConditionQ(alertSearchCondition(debouncedQuery))
        : undefined,
    [debouncedQuery],
  );

  const alertList = Records.useList(
    {
      limit: ALERT_SEARCH_LIMIT,
      orderby: "date_epoch",
      asc: false,
      ...(alertQ ? { q: alertQ } : {}),
    },
    { enabled: open && alertQ !== undefined },
  );

  const entries = useMemo<Entry[]>(() => {
    const q = query.trim().toLowerCase();
    const nav = q ? NAV_ITEMS.filter((i) => i.label.toLowerCase().includes(q)) : NAV_ITEMS;
    const actions = q ? pageActions.filter((a) => a.label.toLowerCase().includes(q)) : pageActions;
    const alerts = alertQ !== undefined ? (alertList.data?.data ?? []) : [];
    return [
      ...nav.map((item): Entry => ({ kind: "nav", item })),
      ...actions.map((action): Entry => ({ kind: "action", action })),
      ...alerts.map((record): Entry => ({ kind: "alert", record })),
    ];
  }, [query, pageActions, alertQ, alertList.data]);

  useEffect(() => {
    setActiveIndex(0);
  }, [query]);

  useEffect(() => {
    if (open) {
      setQuery("");
      setDebouncedQuery("");
      setActiveIndex(0);
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [open]);

  function select(i: number) {
    const entry = entries[i];
    if (!entry) return;
    onOpenChange(false);
    if (entry.kind === "nav") {
      void navigate({ to: entry.item.to });
      return;
    }
    if (entry.kind === "action") {
      entry.action.run();
      return;
    }
    // Land on the alerts page with exactly this record in view and its detail
    // drawer open. The uid filter guarantees the row is on the page (the
    // drawer closes itself when its key isn't in the data) and the "all" tab
    // keeps an already-acked or closed alert from being filtered back out.
    const uid = entry.record.uid;
    if (!uid) return;
    // TanStack Router's navigate types are locked to the registered route tree
    // at build time; cast through unknown the same way AlertsPage does.
    type NavigateFn = (opts: { to: string; search: Record<string, unknown> }) => Promise<void>;
    void (navigate as unknown as NavigateFn)({
      to: "/web/alerts",
      search: { tab: "all", search: `uid = "${uid}"`, record: uid },
    });
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActiveIndex((i) => Math.min(entries.length - 1, i + 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActiveIndex((i) => Math.max(0, i - 1));
    } else if (e.key === "Enter") {
      e.preventDefault();
      select(activeIndex);
    }
  }

  const navEntries = entries.filter((e) => e.kind === "nav");
  const actionEntries = entries.filter((e) => e.kind === "action");
  const alertEntries = entries.filter((e) => e.kind === "alert");
  const visibleGroups = GROUPS.filter((g) =>
    navEntries.some((e) => e.kind === "nav" && e.item.group === g),
  );

  const activeEntry = entries[activeIndex];
  const activeDescendant = activeEntry ? entryId(activeEntry) : undefined;
  const searching = alertQ !== undefined && alertList.isFetching && alertEntries.length === 0;

  function optionLabel(entry: Entry): React.ReactNode {
    if (entry.kind === "nav") {
      return (
        <>
          <Icon name={entry.item.icon} size={16} />
          <span>{entry.item.label}</span>
          {entry.item.shortcut ? (
            <span className={styles.optionShortcut}>{shortcutLabel(entry.item.shortcut)}</span>
          ) : null}
        </>
      );
    }
    if (entry.kind === "action") {
      return (
        <>
          <Icon name={entry.action.icon ?? "check"} size={16} />
          <span>{entry.action.label}</span>
          {entry.action.hint ? (
            <span className={styles.optionShortcut}>{entry.action.hint}</span>
          ) : null}
        </>
      );
    }
    return (
      <>
        <Icon name="file-text" size={16} />
        <span className={styles.alertMessage}>{entry.record.message ?? "(no message)"}</span>
        <span className={styles.optionShortcut}>{entry.record.host ?? "—"}</span>
      </>
    );
  }

  function renderOption(entry: Entry) {
    const globalIndex = entries.indexOf(entry);
    return (
      /* eslint-disable-next-line jsx-a11y/click-events-have-key-events */
      <li
        key={entryId(entry)}
        id={entryId(entry)}
        role="option"
        aria-selected={globalIndex === activeIndex}
        className={styles.option}
        onMouseEnter={() => setActiveIndex(globalIndex)}
        onClick={() => select(globalIndex)}
      >
        {optionLabel(entry)}
      </li>
    );
  }

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
              role="combobox"
              aria-label="Command palette search"
              aria-expanded={entries.length > 0}
              aria-controls={LISTBOX_ID}
              aria-autocomplete="list"
              {...(activeDescendant ? { "aria-activedescendant": activeDescendant } : {})}
              className={styles.search}
              placeholder="Jump to a page, or search alerts…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={onKeyDown}
            />
            <Kbd>Esc</Kbd>
          </div>
          {entries.length === 0 ? (
            <div className={styles.empty}>{searching ? "Searching alerts…" : "No matches"}</div>
          ) : (
            <ul className={styles.list} role="listbox" id={LISTBOX_ID}>
              {visibleGroups.map((group) => (
                <li key={group}>
                  <div className={styles.group}>{GROUP_LABELS[group]}</div>
                  <ul className={styles.sublist}>
                    {navEntries
                      .filter((e) => e.kind === "nav" && e.item.group === group)
                      .map(renderOption)}
                  </ul>
                </li>
              ))}
              {actionEntries.length > 0 ? (
                <li>
                  <div className={styles.group}>On this page</div>
                  <ul className={styles.sublist}>{actionEntries.map(renderOption)}</ul>
                </li>
              ) : null}
              {alertEntries.length > 0 ? (
                <li>
                  <div className={styles.group}>Alerts</div>
                  <ul className={styles.sublist}>{alertEntries.map(renderOption)}</ul>
                </li>
              ) : null}
              {searching ? <li className={styles.searching}>Searching alerts…</li> : null}
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
