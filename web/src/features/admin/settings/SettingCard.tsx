import { useEffect, useMemo, useState } from "react";
import { Button } from "@/shared/ui/Button";
import { MetadataField } from "@/shared/forms/MetadataForm";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import type { FormField } from "@/shared/forms/types";
import { Settings } from "./api";
import styles from "./SettingCard.module.css";

export type SettingCardProps = {
  /** Catalogue descriptor for this setting (component, default_value, etc.). */
  field: FormField;
  /** Setting key (e.g. "ldap.host"). */
  name: string;
  /** Current DB value when a Setting record exists, otherwise undefined. */
  initialValue?: unknown;
  /**
   * UID of the persisted Setting; undefined when the row hasn't been
   * created. Declared as `string | undefined` (not optional `?`) so callers
   * compiled under `exactOptionalPropertyTypes` can pass `undefined`
   * directly without conditional spreads.
   */
  recordUid: string | undefined;
  /** Invalidate the list query so the parent page sees the new state. */
  onChange: () => void;
};

/**
 * Deep equality on plain JSON-ish shapes (primitives, arrays, plain objects).
 * Mirrors what the Reset / dirty check needs: arrays-of-strings (Arguments)
 * and key/value maps (kv Arguments) plus simple primitives.
 */
function deepEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (a === null || b === null) return false;
  if (typeof a !== typeof b) return false;
  if (Array.isArray(a) && Array.isArray(b)) {
    if (a.length !== b.length) return false;
    for (let i = 0; i < a.length; i++) {
      if (!deepEqual(a[i], b[i])) return false;
    }
    return true;
  }
  if (typeof a === "object" && typeof b === "object") {
    const oa = a as Record<string, unknown>;
    const ob = b as Record<string, unknown>;
    const ka = Object.keys(oa);
    const kb = Object.keys(ob);
    if (ka.length !== kb.length) return false;
    for (const k of ka) {
      if (!deepEqual(oa[k], ob[k])) return false;
    }
    return true;
  }
  return false;
}

export function SettingCard({ field, name, initialValue, recordUid, onChange }: SettingCardProps) {
  // Local editor state mirrors `initialValue` until the user edits. Seeded
  // with the catalogue's default when no record exists so the rendered input
  // shows the server-side default rather than an empty control.
  const [value, setValue] = useState<unknown>(
    initialValue === undefined ? field.default_value : initialValue,
  );

  // Resync when the parent reports a new initialValue (e.g. after a Save
  // refetches the list).
  useEffect(() => {
    setValue(initialValue === undefined ? field.default_value : initialValue);
  }, [initialValue, field.default_value]);

  const create = Settings.useCreate();
  const update = Settings.useUpdate();
  const remove = Settings.useRemove();

  // Dirty tracking: compare the typed value against what came from the DB.
  // When no record exists we compare against the catalogue's default so the
  // user can save the default verbatim if they want to (creates a row).
  const baseline = initialValue === undefined ? field.default_value : initialValue;
  const dirty = useMemo(() => !deepEqual(value, baseline), [value, baseline]);

  const fieldId = `setting-${name.replace(/\./g, "-")}`;

  async function handleSave() {
    try {
      if (recordUid === undefined) {
        await create.mutateAsync({ name, value });
      } else {
        await update.mutateAsync({ uid: recordUid, body: { name, value } });
      }
      toast.success(`Saved ${field.display_name}`);
      onChange();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Save failed");
    }
  }

  function handleReset() {
    setValue(baseline);
  }

  async function handleRevert() {
    if (recordUid === undefined) return;
    try {
      await remove.mutateAsync(recordUid);
      toast.success(`Reverted ${field.display_name} to default`);
      onChange();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Revert failed");
    }
  }

  const notSet = recordUid === undefined;
  const submitting = create.isPending || update.isPending || remove.isPending;

  return (
    <section className={styles.card}>
      <div className={styles.header}>
        <label htmlFor={fieldId} className={styles.label} id={`label-${fieldId}`}>
          {field.display_name}
        </label>
        {notSet ? (
          <span className={styles.indicator}>not set</span>
        ) : dirty ? (
          <span className={`${styles.indicator} ${styles.indicatorDirty}`}>modified</span>
        ) : null}
      </div>
      {field.description ? <p className={styles.description}>{field.description}</p> : null}
      <div className={styles.control}>
        <MetadataField id={fieldId} field={field} value={value} onChange={(v) => setValue(v)} />
      </div>
      <div className={styles.actions}>
        {recordUid !== undefined ? (
          <Button
            size="sm"
            variant="ghost"
            className={styles.deleteSpacer}
            onClick={() => void handleRevert()}
            loading={remove.isPending}
            disabled={submitting}
          >
            Revert to default
          </Button>
        ) : null}
        <Button size="sm" variant="ghost" onClick={handleReset} disabled={!dirty || submitting}>
          Reset
        </Button>
        <Button
          size="sm"
          variant="primary"
          onClick={() => void handleSave()}
          loading={create.isPending || update.isPending}
          disabled={!dirty || submitting}
        >
          Save
        </Button>
      </div>
    </section>
  );
}
