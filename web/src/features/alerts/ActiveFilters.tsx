// ActiveFilters — a dismissable chip strip summarising the tab + environment
// constraints currently narrowing the alerts list. Rendered by AlertsPage only
// when at least one of those is active. Each chip removes exactly one
// constraint; the "Clear all" button wipes them (incl. any search) in a single
// navigation.
//
// The DSL search text is deliberately NOT shown here: the SearchBar already
// displays it verbatim and carries its own one-click ✕ clear, so a "Search:"
// chip would just duplicate both. Tab + env, by contrast, are set elsewhere
// (the tab strip and env bar) and earn a dismissable chip.
import { Icon } from "@/shared/icons/Icon";
import { Button } from "@/shared/ui/Button";
import { tabById, type TabId } from "./tabs";
import styles from "./ActiveFilters.module.css";

export type ActiveFiltersProps = {
  /** Active lifecycle tab. The default "alerts" tab renders no chip. */
  tab: TabId;
  /** UIDs of selected environments, in selection order. */
  envs: string[];
  /** Resolves an environment UID to its display name (falls back to the UID). */
  envName: (uid: string) => string;
  /** Remove a single environment from the selection. */
  onRemoveEnv: (uid: string) => void;
  /** Reset the lifecycle tab back to the default "alerts" tab. */
  onClearTab: () => void;
  /** Reset every filter at once (single updateSearch + local-state reset). */
  onClearAll: () => void;
};

function Chip({
  label,
  value,
  onRemove,
}: {
  label: string;
  value: string;
  onRemove: () => void;
}) {
  return (
    <span className={styles.chip}>
      <span className={styles.chipLabel}>{label}</span>
      <span className={styles.chipValue}>{value}</span>
      <button
        type="button"
        className={styles.chipRemove}
        aria-label={`Remove ${label} filter: ${value}`}
        onClick={onRemove}
      >
        <Icon name="x" size={12} />
      </button>
    </span>
  );
}

export function ActiveFilters({
  tab,
  envs,
  envName,
  onRemoveEnv,
  onClearTab,
  onClearAll,
}: ActiveFiltersProps) {
  const showTab = tab !== "alerts";

  return (
    <div className={styles.strip} role="group" aria-label="Active filters">
      {showTab ? <Chip label="Tab" value={tabById(tab).label} onRemove={onClearTab} /> : null}
      {envs.map((uid) => (
        <Chip key={uid} label="Env" value={envName(uid)} onRemove={() => onRemoveEnv(uid)} />
      ))}
      <Button size="sm" variant="ghost" className={styles.clearAll} onClick={onClearAll}>
        Clear all
      </Button>
    </div>
  );
}
