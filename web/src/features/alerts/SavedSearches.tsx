// SavedSearches — a collapsible panel on the Alerts page that lists the
// operator's server-persisted named filters. Clicking a row applies its DSL
// query to the SearchBar (via onApply); an inline "Save current search" form
// captures the current query under a name. Saved searches are scoped per
// tenant + owner server-side, so the list a user sees is their own.
import { useState } from "react";
import { CollapsibleSection } from "@/shared/ui/CollapsibleSection";
import { Button } from "@/shared/ui/Button";
import { IconButton } from "@/shared/ui/IconButton";
import { Input } from "@/shared/ui/Input";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { SavedSearches as SavedSearchResource } from "./savedSearchApi";
import styles from "./SavedSearches.module.css";

export type SavedSearchesProps = {
  /** The SearchBar DSL text currently entered, used by the save form. */
  currentQuery: string;
  /** Apply a stored query to the page (wired to handleSearchSubmit). */
  onApply: (query: string) => void;
};

export function SavedSearches({ currentQuery, onApply }: SavedSearchesProps) {
  const list = SavedSearchResource.useList({ limit: 200, orderby: "name", asc: true });
  const createMut = SavedSearchResource.useCreate();
  const removeMut = SavedSearchResource.useRemove();
  const [name, setName] = useState("");

  const items = list.data?.data ?? [];
  const canSave = currentQuery.trim() !== "";

  const save = () => {
    const trimmed = name.trim();
    if (!trimmed || !canSave) return;
    void (async () => {
      try {
        await createMut.mutateAsync({ name: trimmed, query: currentQuery });
        toast.success(`Saved “${trimmed}”`);
        setName("");
      } catch (e) {
        const detail = e instanceof ApiError ? e.detail : "Could not save search";
        toast.error(detail);
      }
    })();
  };

  const remove = (uid: string | undefined, label: string) => {
    if (!uid) return;
    void (async () => {
      try {
        await removeMut.mutateAsync(uid);
        toast.success(`Deleted “${label}”`);
      } catch (e) {
        const detail = e instanceof ApiError ? e.detail : "Could not delete search";
        toast.error(detail);
      }
    })();
  };

  return (
    <CollapsibleSection
      title="Saved searches"
      summary={items.length > 0 ? String(items.length) : undefined}
    >
      <div className={styles.panel}>
        {items.length === 0 ? (
          <p className={styles.empty}>No saved searches yet.</p>
        ) : (
          <ul className={styles.list}>
            {items.map((s) => (
              <li key={s.uid ?? s.name} className={styles.row}>
                <button
                  type="button"
                  className={styles.apply}
                  title={s.query}
                  aria-label={`Apply ${s.name}`}
                  onClick={() => onApply(s.query)}
                >
                  <span className={styles.name}>{s.name}</span>
                  <span className={styles.query}>{s.query}</span>
                </button>
                <IconButton
                  icon="trash"
                  label={`Delete ${s.name}`}
                  size="sm"
                  variant="ghostDanger"
                  onClick={() => remove(s.uid, s.name)}
                />
              </li>
            ))}
          </ul>
        )}
        {canSave ? (
          <div className={styles.saveForm}>
            <p
              className={styles.empty}
              style={{ flexBasis: "100%", margin: 0, fontSize: "var(--text-xs)" }}
            >
              Saves this query only — not the active tab or environment filter.
            </p>
            <label className={styles.label} htmlFor="saved-search-name">
              Name
            </label>
            <Input
              id="saved-search-name"
              value={name}
              size="sm"
              placeholder="e.g. prod criticals unacked"
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  save();
                }
              }}
            />
            <Button
              size="sm"
              variant="secondary"
              leadingIcon="save"
              disabled={name.trim() === "" || createMut.isPending}
              onClick={save}
            >
              Save
            </Button>
          </div>
        ) : null}
      </div>
    </CollapsibleSection>
  );
}
