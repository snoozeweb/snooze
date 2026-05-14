import { Combobox } from "@/shared/ui/Combobox";
import { IconButton } from "@/shared/ui/IconButton";
import { Input } from "@/shared/ui/Input";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/shared/ui/Select";
import { OPERATORS, defaultValueForOp, valueShapeForOp } from "@/lib/condition/operators";
import type { Condition, ConditionType } from "@/lib/condition/types";
import styles from "./ConditionEditor.module.css";

export type ConditionLeafProps = {
  value: Condition;
  fieldOptions: string[];
  onChange: (next: Condition) => void;
  onDelete: () => void;
};

export function ConditionLeaf({ value, fieldOptions, onChange, onDelete }: ConditionLeafProps) {
  const shape = valueShapeForOp(value.type);
  const opOptions = OPERATORS.map((o) => ({ value: o.type, label: o.label }));

  function setOperator(newType: ConditionType) {
    if (newType === "AND" || newType === "OR" || newType === "NOT") return;
    const field = "field" in value ? value.field : "";
    const next = defaultValueForOp(newType);
    if (newType === "ALWAYS_TRUE") {
      onChange({ type: "ALWAYS_TRUE" });
      return;
    }
    if (newType === "EXISTS") {
      onChange({ type: "EXISTS", field });
      return;
    }
    const newShape = valueShapeForOp(newType);
    if (newShape === "string") {
      onChange({
        type: newType as "EQUALS" | "CONTAINS" | "MATCHES" | "SEARCH",
        field,
        value: (next as string) ?? "",
      });
      return;
    }
    if (newShape === "number") {
      onChange({
        type: newType as "LT" | "GT" | "LE" | "GE",
        field,
        value: (next as number) ?? 0,
      });
      return;
    }
    if (newShape === "array") {
      onChange({
        type: "IN",
        field,
        value: (next as string[]) ?? [],
      });
      return;
    }
  }

  function setField(field: string) {
    if (
      value.type === "ALWAYS_TRUE" ||
      value.type === "NOT" ||
      value.type === "AND" ||
      value.type === "OR"
    )
      return;
    // Cast to the leaf subset so the spread of `field` type-checks cleanly
    const leaf = value as Extract<Condition, { field: string }>;
    onChange({ ...leaf, field });
  }

  function setStringValue(v: string) {
    if (
      value.type === "EQUALS" ||
      value.type === "CONTAINS" ||
      value.type === "MATCHES" ||
      value.type === "SEARCH"
    ) {
      onChange({ ...value, value: v });
    }
  }

  function setNumberValue(v: number) {
    if (value.type === "LT" || value.type === "GT" || value.type === "LE" || value.type === "GE") {
      onChange({ ...value, value: v });
    }
  }

  function setArrayValue(v: string) {
    if (value.type === "IN") {
      const arr = v
        .split(",")
        .map((s) => s.trim())
        .filter((s) => s.length > 0);
      onChange({ ...value, value: arr });
    }
  }

  return (
    <div className={styles.leaf}>
      {value.type !== "ALWAYS_TRUE" ? (
        <div className={styles.field}>
          <Combobox
            options={fieldOptions.map((f) => ({ value: f, label: f }))}
            value={"field" in value ? value.field : ""}
            onValueChange={setField}
            placeholder="field"
          />
        </div>
      ) : null}
      <div className={styles.op}>
        <Select value={value.type} onValueChange={(v) => setOperator(v as ConditionType)}>
          <SelectTrigger placeholder="op" />
          <SelectContent>
            {opOptions.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className={styles.value}>
        {shape === "string" &&
        (value.type === "EQUALS" ||
          value.type === "CONTAINS" ||
          value.type === "MATCHES" ||
          value.type === "SEARCH") ? (
          <Input
            value={value.value}
            onChange={(e) => setStringValue(e.target.value)}
            placeholder="value"
          />
        ) : null}
        {shape === "number" &&
        (value.type === "LT" ||
          value.type === "GT" ||
          value.type === "LE" ||
          value.type === "GE") ? (
          <Input
            type="number"
            value={String(value.value)}
            onChange={(e) => setNumberValue(Number(e.target.value))}
            placeholder="number"
          />
        ) : null}
        {shape === "array" && value.type === "IN" ? (
          <Input
            value={value.value.join(", ")}
            onChange={(e) => setArrayValue(e.target.value)}
            placeholder="comma-separated values"
          />
        ) : null}
      </div>
      <div className={styles.actions}>
        <IconButton icon="trash" label="Remove" variant="ghost" size="sm" onClick={onDelete} />
      </div>
    </div>
  );
}
