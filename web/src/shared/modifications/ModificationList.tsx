import { Button } from "@/shared/ui/Button";
import { IconButton } from "@/shared/ui/IconButton";
import { Input } from "@/shared/ui/Input";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/shared/ui/Select";
import {
  MODIFICATION_TYPES,
  defaultModification,
  type Modification,
  type ModificationType,
} from "./types";
import styles from "./ModificationList.module.css";

export type ModificationListProps = {
  value: Modification[];
  onChange: (next: Modification[]) => void;
};

export function ModificationList({ value, onChange }: ModificationListProps) {
  function update(i: number, patch: Partial<Modification>) {
    const next = value.slice();
    const current = next[i];
    if (!current) return;
    next[i] = { ...current, ...patch } as Modification;
    onChange(next);
  }

  function setType(i: number, type: ModificationType) {
    const next = value.slice();
    const old = next[i];
    if (!old) return;
    const fresh = defaultModification(type);
    if ("field" in old) {
      (fresh as { field: string }).field = old.field;
    }
    next[i] = fresh;
    onChange(next);
  }

  function remove(i: number) {
    onChange(value.filter((_, k) => k !== i));
  }

  function add() {
    onChange([...value, defaultModification("set")]);
  }

  return (
    <div className={styles.list}>
      {value.length === 0 ? <p className={styles.empty}>No modifications.</p> : null}
      {value.map((mod, i) => (
        <div key={i} className={styles.row}>
          <div className={styles.type}>
            <Select value={mod.type} onValueChange={(v) => setType(i, v as ModificationType)}>
              <SelectTrigger placeholder="type" />
              <SelectContent>
                {MODIFICATION_TYPES.map((m) => (
                  <SelectItem key={m.value} value={m.value}>
                    {m.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className={styles.field}>
            <Input
              placeholder="field"
              value={mod.field}
              onChange={(e) => update(i, { field: e.target.value } as Partial<Modification>)}
            />
          </div>
          {mod.type === "set" || mod.type === "array_append" ? (
            <div className={styles.value}>
              <Input
                placeholder={mod.type === "array_append" ? "value to append" : "value"}
                value={mod.value}
                onChange={(e) => update(i, { value: e.target.value } as Partial<Modification>)}
              />
            </div>
          ) : null}
          {mod.type === "regex_sub" ? (
            <>
              <div className={styles.value}>
                <Input
                  placeholder="pattern"
                  value={mod.pattern}
                  onChange={(e) => update(i, { pattern: e.target.value } as Partial<Modification>)}
                />
              </div>
              <div className={styles.value}>
                <Input
                  placeholder="replace"
                  value={mod.replace}
                  onChange={(e) => update(i, { replace: e.target.value } as Partial<Modification>)}
                />
              </div>
            </>
          ) : null}
          <div className={styles.actions}>
            <IconButton
              icon="trash"
              label="Remove"
              variant="ghost"
              size="sm"
              onClick={() => remove(i)}
            />
          </div>
        </div>
      ))}
      <Button
        size="sm"
        variant="secondary"
        leadingIcon="plus"
        onClick={add}
        className={styles.addBtn}
      >
        Add modification
      </Button>
    </div>
  );
}
