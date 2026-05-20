import type { ReactNode } from "react";
import { SearchBar, type ParsedCondition, type ParseError } from "@/shared/ui/SearchBar";
import { EnvironmentBar } from "./EnvironmentBar";
import { ALERT_TABS, type TabId } from "./tabs";
import styles from "./Filters.module.css";

export type AlertFilters = {
  /** Active lifecycle tab. Defaults to "alerts" when unset. */
  tab?: TabId;
  /** Raw query string from the SearchBar (URL-persisted). */
  search?: string;
  /**
   * Parsed AST from the SearchBar, written through onChange whenever the
   * server parse round-trip completes. AlertsPage combines this with the
   * active tab's preset into the final ?q= condition. Null = no DSL filter.
   */
  searchCondition?: ParsedCondition | null;
  /** Last parse error, surfaced in the SearchBar pill. */
  searchError?: ParseError | null;
  /**
   * UIDs of currently selected environments. AlertsPage resolves these to
   * their stored `condition`s and OR's them, then AND's with the tab and
   * DSL search to produce the final ?q=.
   */
  envs?: string[];
};

export type AlertsFiltersProps = {
  value: AlertFilters;
  onChange: (next: AlertFilters) => void;
  /** Optional content rendered to the right of the SearchBar on the same
   *  row — used by AlertsPage to host the count + auto-refresh toggle so
   *  every "table chrome" affordance lines up on a single horizontal row. */
  rightSlot?: ReactNode;
  /** Count text rendered between the SearchBar and `rightSlot`. Mirrors
   *  DataTable's `toolbarHeader` so the surface reads identically across
   *  pages. */
  countLabel?: ReactNode;
};

/**
 * AlertsFilters renders the alerts page header: a horizontal tab strip
 * keyed by lifecycle state, plus the search-DSL editor.
 *
 * The seven tabs mirror the Python 1.x web UI (see tabs.ts). Each tab
 * applies a preset Condition that AND-combines with the SearchBar's DSL
 * condition in AlertsPage. The combined Cond is sent server-side as
 * `?q=base64url(JSON)`.
 *
 * No standalone state/severity/environment selects — the DSL covers them
 * (`severity = critical AND environment = prod`).
 */
export function AlertsFilters({ value, onChange, rightSlot, countLabel }: AlertsFiltersProps) {
  const activeTab: TabId = value.tab ?? "alerts";

  function handleTab(id: TabId) {
    if (id === activeTab) return;
    onChange({ ...value, tab: id });
  }

  return (
    <div className={styles.bar}>
      <EnvironmentBar
        selected={value.envs ?? []}
        onChange={(envs) => onChange({ ...value, envs })}
      />
      <div role="tablist" aria-label="Alert lifecycle filter" className={styles.tabs}>
        {ALERT_TABS.map((tab) => {
          const active = tab.id === activeTab;
          return (
            <button
              key={tab.id}
              type="button"
              role="tab"
              aria-selected={active}
              data-state={active ? "active" : "inactive"}
              className={styles.tab}
              onClick={() => handleTab(tab.id)}
            >
              {tab.label}
            </button>
          );
        })}
      </div>
      <div className={styles.searchRow}>
        <div className={styles.searchSlot}>
          <SearchBar
            value={value.search ?? ""}
            onChange={(c) => {
              const next: AlertFilters = { ...value };
              if (c.text) next.search = c.text;
              else delete next.search;
              next.searchCondition = c.condition;
              next.searchError = c.error;
              onChange(next);
            }}
          />
        </div>
        {countLabel !== undefined ? <span className={styles.count}>{countLabel}</span> : null}
        {rightSlot !== undefined ? <div className={styles.actions}>{rightSlot}</div> : null}
      </div>
    </div>
  );
}
