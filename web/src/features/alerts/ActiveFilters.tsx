// ActiveFilters — a dismissable chip strip summarising every filter currently
// narrowing the alerts list. Rendered by AlertsPage only when
// `hasActiveFilters` is true. Each chip removes exactly one constraint; the
// "Clear all" button wipes them in a single navigation.
//
// The three filter sources live in different places (tab + env in the URL,
// the DSL search text in local React state because navigate() is async — see
// the comment block in AlertsPage), so this component takes one remove
// callback per source rather than owning any state itself.
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
  /** Current DSL search text. Empty/whitespace renders no chip. */
  search: string;
  /** Remove a single environment from the selection. */
  onRemoveEnv: (uid: string) => void;
  /** Reset the lifecycle tab back to the default "alerts" tab. */
  onClearTab: () => void;
  /** Clear the DSL search text + parsed condition. */
  onClearSearch: () => void;
  /** Reset every filter at once (single updateSearch + local-state reset). */
  onClearAll: () => void;
};

function Chip({
  label,
  value,
  onRemove,
  mono,
}: {
  label: string;
  value: string;
  onRemove: () => void;
  mono?: boolean;
}) {
  return (
    <span className={styles.chip}>
      <span className={styles.chipLabel}>{label}</span>
      <span className={mono ? styles.chipValueMono : styles.chipValue}>{value}</span>
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
  search,
  onRemoveEnv,
  onClearTab,
  onClearSearch,
  onClearAll,
}: ActiveFiltersProps) {
  const trimmedSearch = search.trim();
  const showTab = tab !== "alerts";

  return (
    <div className={styles.strip} role="group" aria-label="Active filters">
      {showTab ? <Chip label="Tab" value={tabById(tab).label} onRemove={onClearTab} /> : null}
      {envs.map((uid) => (
        <Chip key={uid} label="Env" value={envName(uid)} onRemove={() => onRemoveEnv(uid)} />
      ))}
      {trimmedSearch ? (
        <Chip label="Search" value={trimmedSearch} onRemove={onClearSearch} mono />
      ) : null}
      <Button size="sm" variant="ghost" className={styles.clearAll} onClick={onClearAll}>
        Clear all
      </Button>
    </div>
  );
}
