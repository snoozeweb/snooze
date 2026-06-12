import { EnvironmentBar } from "./EnvironmentBar";
import { ALERT_TABS, type TabId } from "./tabs";
import styles from "./Filters.module.css";

export type AlertFilters = {
  /** Active lifecycle tab. Defaults to "alerts" when unset. */
  tab?: TabId;
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
};

/**
 * AlertsFilters renders the alerts page header: a horizontal tab strip
 * keyed by lifecycle state plus the environment pill bar.
 *
 * The seven tabs mirror the Python 1.x web UI (see tabs.ts). Each tab
 * applies a preset Condition that AND-combines with the SearchBar's DSL
 * condition in AlertsPage. The combined Cond is sent server-side as
 * `?q=base64url(JSON)`.
 *
 * The SearchBar lives on DataTable.search (not here) so the bulk-action
 * toolbar that appears on row selection shares the row with the search
 * box — matching every other list page. Count + auto-refresh toggle move
 * to DataTable.toolbar for the same reason.
 */
export function AlertsFilters({ value, onChange }: AlertsFiltersProps) {
  const activeTab: TabId = value.tab ?? "alerts";

  function handleTab(id: TabId) {
    if (id === activeTab) return;
    onChange({ ...value, tab: id });
  }

  return (
    <div className={styles.bar}>
      <div className={styles.tabRow}>
        <div role="tablist" aria-label="Alert lifecycle filter" className={styles.tabs}>
          {ALERT_TABS.map((tab) => {
            const active = tab.id === activeTab;
            return (
              <button
                key={tab.id}
                type="button"
                role="tab"
                id={`alerts-tab-${tab.id}`}
                aria-selected={active}
                aria-controls="alerts-panel"
                data-state={active ? "active" : "inactive"}
                className={styles.tab}
                onClick={() => handleTab(tab.id)}
              >
                {tab.label}
              </button>
            );
          })}
        </div>
        <div className={styles.envSlot}>
          <EnvironmentBar
            selected={value.envs ?? []}
            onChange={(envs) => onChange({ ...value, envs })}
          />
        </div>
      </div>
    </div>
  );
}
