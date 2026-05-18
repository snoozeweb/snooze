// ConditionNode — unified recursive editor for one condition (leaf or
// logic group). Mirrors the legacy Vue ConditionChild.vue: every node
// owns an operator dropdown (for logic) or a field/op/value triplet (for
// leaves), a (+) button to add a child or fork into AND, and a trash
// button that delegates removal upward (root collapses to ALWAYS_TRUE).
import { Combobox } from "@/shared/ui/Combobox";
import { IconButton } from "@/shared/ui/IconButton";
import { Input } from "@/shared/ui/Input";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/shared/ui/Select";
import { OPERATORS, valueShapeForOp } from "@/lib/condition/operators";
import type { Condition, ConditionType } from "@/lib/condition/types";
import styles from "./ConditionEditor.module.css";

export type ConditionNodeProps = {
  value: Condition;
  fieldOptions: string[];
  onChange: (next: Condition) => void;
  onDelete?: () => void;
  isRoot?: boolean;
};

const LOGIC_OPTIONS: { value: "AND" | "OR" | "NOT"; label: string }[] = [
  { value: "AND", label: "AND" },
  { value: "OR", label: "OR" },
  { value: "NOT", label: "NOT" },
];

const LEAF_OPTIONS = OPERATORS.filter((o) => o.type !== "ALWAYS_TRUE").map((o) => ({
  value: o.type,
  label: o.label,
}));

function defaultLeaf(): Condition {
  return { type: "EQUALS", field: "", value: "" };
}

function isLogic(c: Condition): c is { type: "AND" | "OR"; args: Condition[] } {
  return c.type === "AND" || c.type === "OR";
}

function childArgsOf(c: Condition): Condition[] {
  if (c.type === "NOT") return [c.arg];
  if (isLogic(c)) return c.args;
  return [];
}

export function ConditionNode({
  value,
  fieldOptions,
  onChange,
  onDelete,
  isRoot = false,
}: ConditionNodeProps) {
  // ── Empty (ALWAYS_TRUE) ────────────────────────────────────────
  if (value.type === "ALWAYS_TRUE") {
    return (
      <div className={styles.empty}>
        <span>Always (matches all alerts).</span>
        <IconButton
          icon="plus"
          label="Add filter"
          variant="secondary"
          size="sm"
          onClick={() => onChange({ type: "AND", args: [defaultLeaf(), defaultLeaf()] })}
        />
      </div>
    );
  }

  function handleRootOrEscalateDelete() {
    if (isRoot) {
      onChange({ type: "ALWAYS_TRUE" });
    } else {
      onDelete?.();
    }
  }

  // ── Logic node (AND / OR / NOT) ────────────────────────────────
  if (value.type === "AND" || value.type === "OR" || value.type === "NOT") {
    // Capture the narrowed value in a typed alias so the closures below
    // keep their type information (TS widens inside nested functions).
    const logic:
      | { type: "AND"; args: Condition[] }
      | { type: "OR"; args: Condition[] }
      | { type: "NOT"; arg: Condition } = value;
    const children = childArgsOf(logic);

    function changeLogicOp(nextOp: "AND" | "OR" | "NOT") {
      if (nextOp === "NOT") {
        // NOT takes a single arg — keep the first child, drop the rest.
        const first = children[0] ?? defaultLeaf();
        onChange({ type: "NOT", arg: first });
        return;
      }
      // AND/OR need at least two args so the editor stays paired.
      let args = children.slice();
      if (args.length < 2) args = [...args, defaultLeaf()];
      onChange({ type: nextOp, args });
    }

    function addChild() {
      // Append a fresh leaf. (Disabled for NOT — single-arg only.)
      if (logic.type === "NOT") return;
      onChange({ type: logic.type, args: [...logic.args, defaultLeaf()] });
    }

    function updateChild(i: number, next: Condition) {
      if (logic.type === "NOT") {
        if (i !== 0) return;
        onChange({ type: "NOT", arg: next });
        return;
      }
      const args = logic.args.slice();
      args[i] = next;
      onChange({ type: logic.type, args });
    }

    function deleteChild(i: number) {
      if (logic.type === "NOT") {
        // Removing the only child of NOT collapses the whole NOT away.
        handleRootOrEscalateDelete();
        return;
      }
      const nextArgs = logic.args.filter((_, k) => k !== i);
      if (nextArgs.length === 0) {
        handleRootOrEscalateDelete();
        return;
      }
      if (nextArgs.length === 1) {
        // Collapse: replace the group with its single remaining child so
        // the user doesn't end up staring at a one-armed AND.
        const only = nextArgs[0];
        if (only !== undefined) onChange(only);
        return;
      }
      onChange({ type: logic.type, args: nextArgs });
    }

    return (
      <div className={styles.group}>
        <div className={styles.groupHeader}>
          <div className={styles.opSelect}>
            <Select
              value={logic.type}
              onValueChange={(v) => changeLogicOp(v as "AND" | "OR" | "NOT")}
            >
              <SelectTrigger />
              <SelectContent>
                {LOGIC_OPTIONS.map((o) => (
                  <SelectItem key={o.value} value={o.value}>
                    {o.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {logic.type !== "NOT" ? (
            <IconButton
              icon="plus"
              label="Add filter"
              variant="ghost"
              size="sm"
              onClick={addChild}
            />
          ) : null}
          <IconButton
            icon="trash"
            label={isRoot ? "Clear" : "Remove group"}
            variant="ghost"
            size="sm"
            onClick={handleRootOrEscalateDelete}
          />
        </div>
        <ul className={styles.children}>
          {children.map((child, i) => (
            <li key={i} className={styles.childRow}>
              <ConditionNode
                value={child}
                fieldOptions={fieldOptions}
                onChange={(next) => updateChild(i, next)}
                onDelete={() => deleteChild(i)}
              />
            </li>
          ))}
        </ul>
      </div>
    );
  }

  // ── Leaf (EQUALS / CONTAINS / EXISTS / IN / numeric) ───────────
  const leaf = value;
  const shape = valueShapeForOp(leaf.type);
  const fieldText = "field" in leaf ? leaf.field : "";

  function setField(field: string) {
    if (
      leaf.type === "EQUALS" ||
      leaf.type === "CONTAINS" ||
      leaf.type === "MATCHES" ||
      leaf.type === "SEARCH"
    ) {
      onChange({ ...leaf, field });
      return;
    }
    if (leaf.type === "IN") {
      onChange({ ...leaf, field });
      return;
    }
    if (
      leaf.type === "LT" ||
      leaf.type === "GT" ||
      leaf.type === "LE" ||
      leaf.type === "GE"
    ) {
      onChange({ ...leaf, field });
      return;
    }
    if (leaf.type === "EXISTS") {
      onChange({ type: "EXISTS", field });
    }
  }

  function setOperator(nextType: ConditionType) {
    if (nextType === "AND" || nextType === "OR" || nextType === "NOT") return;
    if (nextType === "ALWAYS_TRUE") {
      onChange({ type: "ALWAYS_TRUE" });
      return;
    }
    if (nextType === "EXISTS") {
      onChange({ type: "EXISTS", field: fieldText });
      return;
    }
    const newShape = valueShapeForOp(nextType);
    if (newShape === "string") {
      onChange({
        type: nextType as "EQUALS" | "CONTAINS" | "MATCHES" | "SEARCH",
        field: fieldText,
        value: "",
      });
      return;
    }
    if (newShape === "number") {
      onChange({
        type: nextType as "LT" | "GT" | "LE" | "GE",
        field: fieldText,
        value: 0,
      });
      return;
    }
    if (newShape === "array") {
      onChange({ type: "IN", field: fieldText, value: [] });
    }
  }

  function setStringValue(v: string) {
    if (
      leaf.type === "EQUALS" ||
      leaf.type === "CONTAINS" ||
      leaf.type === "MATCHES" ||
      leaf.type === "SEARCH"
    ) {
      onChange({ ...leaf, value: v });
    }
  }

  function setNumberValue(v: number) {
    if (leaf.type === "LT" || leaf.type === "GT" || leaf.type === "LE" || leaf.type === "GE") {
      onChange({ ...leaf, value: v });
    }
  }

  function setArrayValue(v: string) {
    if (leaf.type === "IN") {
      const arr = v
        .split(",")
        .map((s) => s.trim())
        .filter((s) => s.length > 0);
      onChange({ ...leaf, value: arr });
    }
  }

  function fork() {
    // (+) on a leaf wraps it in a new AND group alongside a fresh leaf —
    // matches the Vue UI exactly.
    onChange({ type: "AND", args: [leaf, defaultLeaf()] });
  }

  return (
    <div className={styles.leaf}>
      <div className={styles.field}>
        <Combobox
          options={fieldOptions.map((f) => ({ value: f, label: f }))}
          value={fieldText}
          onValueChange={setField}
          placeholder="field"
        />
      </div>
      <div className={styles.op}>
        <Select value={leaf.type} onValueChange={(v) => setOperator(v as ConditionType)}>
          <SelectTrigger placeholder="op" />
          <SelectContent>
            {LEAF_OPTIONS.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className={styles.value}>
        {shape === "string" &&
        (leaf.type === "EQUALS" ||
          leaf.type === "CONTAINS" ||
          leaf.type === "MATCHES" ||
          leaf.type === "SEARCH") ? (
          <Input
            value={leaf.value}
            onChange={(e) => setStringValue(e.target.value)}
            placeholder="value"
          />
        ) : null}
        {shape === "number" &&
        (leaf.type === "LT" ||
          leaf.type === "GT" ||
          leaf.type === "LE" ||
          leaf.type === "GE") ? (
          <Input
            type="number"
            value={String(leaf.value)}
            onChange={(e) => setNumberValue(Number(e.target.value))}
            placeholder="number"
          />
        ) : null}
        {shape === "array" && leaf.type === "IN" ? (
          <Input
            value={leaf.value.join(", ")}
            onChange={(e) => setArrayValue(e.target.value)}
            placeholder="comma-separated values"
          />
        ) : null}
      </div>
      <div className={styles.actions}>
        <IconButton
          icon="plus"
          label="Add filter"
          variant="ghost"
          size="sm"
          onClick={fork}
        />
        <IconButton
          icon="trash"
          label="Remove"
          variant="ghost"
          size="sm"
          onClick={handleRootOrEscalateDelete}
        />
      </div>
    </div>
  );
}
