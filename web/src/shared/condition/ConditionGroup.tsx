import { IconButton } from "@/shared/ui/IconButton";
import { Tooltip } from "@/shared/ui/Tooltip";
import type { Condition, GroupOp } from "@/lib/condition/types";
import { ConditionLeaf } from "./ConditionLeaf";
import styles from "./ConditionEditor.module.css";

export type ConditionGroupProps = {
  value: Condition;
  fieldOptions: string[];
  onChange: (next: Condition) => void;
  onDelete?: () => void;
  isRoot?: boolean;
};

function isGroup(c: Condition): c is { type: GroupOp; args: Condition[] } {
  return c.type === "AND" || c.type === "OR";
}

export function ConditionGroup({
  value,
  fieldOptions,
  onChange,
  onDelete,
  isRoot = false,
}: ConditionGroupProps) {
  if (value.type === "NOT") {
    return (
      <div className={styles.group}>
        <div className={styles.groupHeader}>
          <span className={styles.notBadge}>NOT</span>
          {onDelete ? (
            <IconButton
              icon="trash"
              label="Remove NOT"
              variant="ghost"
              size="sm"
              onClick={onDelete}
            />
          ) : null}
        </div>
        <ConditionGroup
          value={value.arg}
          fieldOptions={fieldOptions}
          onChange={(next) => onChange({ type: "NOT", arg: next })}
        />
      </div>
    );
  }

  if (!isGroup(value)) {
    return (
      <ConditionLeaf
        value={value}
        fieldOptions={fieldOptions}
        onChange={onChange}
        onDelete={onDelete ?? (() => undefined)}
      />
    );
  }

  const group = value;

  function toggleType() {
    onChange({ type: group.type === "AND" ? "OR" : "AND", args: group.args });
  }

  function updateChild(i: number, next: Condition) {
    const args = group.args.slice();
    args[i] = next;
    onChange({ ...group, args });
  }

  function removeChild(i: number) {
    onChange({ ...group, args: group.args.filter((_, k) => k !== i) });
  }

  function addLeaf() {
    const leaf: Condition = { type: "EQUALS", field: "", value: "" };
    onChange({ ...group, args: [...group.args, leaf] });
  }

  function addNestedGroup() {
    const sub: Condition = {
      type: "AND",
      args: [{ type: "EQUALS", field: "", value: "" }],
    };
    onChange({ ...group, args: [...group.args, sub] });
  }

  return (
    <div className={styles.group}>
      <div className={styles.groupHeader}>
        <Tooltip
          content={group.type === "AND" ? "All conditions must match" : "Any condition can match"}
        >
          <button type="button" className={styles.groupPill} onClick={toggleType}>
            {group.type}
          </button>
        </Tooltip>
        <IconButton icon="plus" label="Add filter" variant="ghost" size="sm" onClick={addLeaf} />
        <IconButton
          icon="layers"
          label="Add group"
          variant="ghost"
          size="sm"
          onClick={addNestedGroup}
        />
        {!isRoot && onDelete ? (
          <IconButton
            icon="trash"
            label="Remove group"
            variant="ghost"
            size="sm"
            onClick={onDelete}
          />
        ) : null}
      </div>
      {group.args.map((child, i) => (
        <ConditionGroup
          key={i}
          value={child}
          fieldOptions={fieldOptions}
          onChange={(next) => updateChild(i, next)}
          onDelete={() => removeChild(i)}
        />
      ))}
    </div>
  );
}
