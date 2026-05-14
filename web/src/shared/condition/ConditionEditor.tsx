import { Button } from "@/shared/ui/Button";
import type { Condition } from "@/lib/condition/types";
import { ConditionGroup } from "./ConditionGroup";
import { useFieldSuggestions } from "./useFieldSuggestions";
import styles from "./ConditionEditor.module.css";

export type ConditionEditorProps = {
  value: Condition;
  onChange: (next: Condition) => void;
  plugin: string;
};

export function ConditionEditor({ value, onChange, plugin }: ConditionEditorProps) {
  const { fields } = useFieldSuggestions(plugin);

  if (value.type === "ALWAYS_TRUE") {
    return (
      <div className={styles.editor}>
        <div className={styles.empty}>
          <span>Always (matches all rows).</span>
          <Button
            size="sm"
            variant="secondary"
            leadingIcon="plus"
            onClick={() =>
              onChange({
                type: "AND",
                args: [{ type: "EQUALS", field: "", value: "" }],
              })
            }
          >
            Add filter
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className={styles.editor}>
      <ConditionGroup value={value} fieldOptions={fields} onChange={onChange} isRoot />
    </div>
  );
}
