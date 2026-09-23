import { EnvironmentBar } from "./EnvironmentBar";
import { OwnerFilter } from "./OwnerFilter";
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
  /**
   * Selected owner tokens (logins, `~none` for Unowned). AlertsPage OR's
   * them into one condition and AND's it with the rest — see ownerFilter.ts.
   */
  owners?: string[];
};

export type AlertsFiltersProps = {
  value: AlertFilters;
  onChange: (next: AlertFilters) => void;
  /** The owner chips' counting condition: the page's query minus the owner
   *  filter itself (base64url). Undefined counts every record. */
  ownerCountsQ?: string | undefined;
  /** Poll the owner counts with the list. */
  refetchIntervalMs?: number | undefined;
};

/**
 * AlertsFilters renders the alerts page header: a horizontal tab strip
 * keyed by lifecycle state plus the environment pill bar.
 *
 * The lifecycle tabs (see tabs.ts) each apply a preset Condition that
 * AND-combines with the SearchBar's DSL condition in AlertsPage. The combined
 * Cond is sent server-side as `?q=base64url(JSON)`. The owner strip sits right
 * of the tabs — it narrows the same list, by who is working on it — and the
 * environment pills close the row.
 *
 * The SearchBar lives on DataTable.search (not here) so the bulk-action
 * toolbar that appears on row selection shares the row with the search
 * box — matching every other list page. Count + auto-refresh toggle move
 * to DataTable.toolbar for the same reason.
 */
export function AlertsFilters({
  value,
  onChange,
  ownerCountsQ,
  refetchIntervalMs,
}: AlertsFiltersProps) {
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
        <div className={styles.ownerSlot}>
          <OwnerFilter
            value={value.owners ?? []}
            onChange={(owners) => onChange({ ...value, owners })}
            countsQ={ownerCountsQ}
            refetchIntervalMs={refetchIntervalMs}
          />
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
