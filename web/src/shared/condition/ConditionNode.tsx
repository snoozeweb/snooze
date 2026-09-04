// ConditionNode — unified recursive editor for one condition (leaf or
// logic group). Mirrors the legacy Vue ConditionChild.vue: every node
// owns an operator dropdown (for logic) or a field/op/value triplet (for
// leaves), a (+) button to add a child or fork into AND, and a trash
// button that delegates removal upward (root collapses to ALWAYS_TRUE).
import { useRef } from "react";
import { IconButton } from "@/shared/ui/IconButton";
import { Input } from "@/shared/ui/Input";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/shared/ui/Select";
import { OPERATORS, valueShapeForOp } from "@/lib/condition/operators";
import { switchLogicOp } from "./logicOps";
import type { Condition, ConditionType } from "@/lib/condition/types";
import styles from "./ConditionEditor.module.css";

export type ConditionNodeProps = {
  value: Condition;
  fieldOptions: string[];
  onChange: (next: Condition) => void;
  onDelete?: () => void;
  /** Clones this node's subtree and inserts it as a sibling immediately
   *  after this position. Supplied by the parent node — absent on the root. */
  onDuplicate?: () => void;
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

/** Deep-clone a Condition node so the duplicate is structurally independent. */
function cloneCondition(c: Condition): Condition {
  return JSON.parse(JSON.stringify(c)) as Condition;
}

export function ConditionNode({
  value,
  fieldOptions,
  onChange,
  onDelete,
  onDuplicate,
  isRoot = false,
}: ConditionNodeProps) {
  // Remembers the last operand typed for each value shape so that hopping
  // between operators (e.g. GT -> CONTAINS -> GT) restores what was there
  // before instead of re-clearing it every time. Keyed by shape rather than
  // by operator since e.g. CONTAINS and MATCHES already share a value via
  // the same-shape carry-over in setOperator below. Lives for this node's
  // lifetime only — never persisted, never read by anything else.
  const rememberedValues = useRef<{ string?: string; number?: number; array?: string[] }>({});

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
          onClick={() => onChange(defaultLeaf())}
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
      // Switching a multi-child group to NOT negates the whole group rather than
      // discarding all but the first child (see switchLogicOp) — no data loss.
      onChange(switchLogicOp(logic, nextOp, defaultLeaf));
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

    function duplicateChild(i: number) {
      if (logic.type === "NOT") return;
      const clone = cloneCondition(logic.args[i]!);
      const next = [...logic.args.slice(0, i + 1), clone, ...logic.args.slice(i + 1)];
      onChange({ type: logic.type, args: next });
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
          {onDuplicate ? (
            <IconButton
              icon="copy"
              label="Duplicate group"
              variant="ghost"
              size="sm"
              onClick={onDuplicate}
            />
          ) : null}
          <IconButton
            icon="trash"
            label={isRoot ? "Clear" : "Remove group"}
            variant="ghostDanger"
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
                {...(logic.type !== "NOT" ? { onDuplicate: () => duplicateChild(i) } : {})}
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
    // SEARCH has no field input (masked below) — it's never reachable here.
    if (
      leaf.type === "EQUALS" ||
      leaf.type === "NOT_EQUALS" ||
      leaf.type === "CONTAINS" ||
      leaf.type === "MATCHES"
    ) {
      onChange({ ...leaf, field });
      return;
    }
    if (leaf.type === "IN") {
      onChange({ ...leaf, field });
      return;
    }
    if (leaf.type === "LT" || leaf.type === "GT" || leaf.type === "LE" || leaf.type === "GE") {
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
    if (nextType === "SEARCH") {
      // SEARCH only ever reads its value (a full-text term across the whole
      // record) — the backend requires field to stay empty. Force it blank
      // here rather than carrying over whatever field was set for the
      // previous operator, which would silently never match.
      onChange({ type: "SEARCH", field: "", value: "" });
      return;
    }
    const newShape = valueShapeForOp(nextType);
    const oldShape = valueShapeForOp(leaf.type);
    // Stash the operand we're leaving behind so switching back to this shape
    // later (even via an unrelated shape in between) restores it.
    if (oldShape === "string") rememberedValues.current.string = (leaf as { value: string }).value;
    if (oldShape === "number") rememberedValues.current.number = (leaf as { value: number }).value;
    if (oldShape === "array") rememberedValues.current.array = (leaf as { value: string[] }).value;
    if (newShape === "string") {
      onChange({
        type: nextType as "EQUALS" | "NOT_EQUALS" | "CONTAINS" | "MATCHES",
        field: fieldText,
        value: rememberedValues.current.string ?? "",
      });
      return;
    }
    if (newShape === "number") {
      onChange({
        type: nextType as "LT" | "GT" | "LE" | "GE",
        field: fieldText,
        value: rememberedValues.current.number ?? 0,
      });
      return;
    }
    if (newShape === "array") {
      onChange({
        type: "IN",
        field: fieldText,
        value: rememberedValues.current.array ?? [],
      });
    }
  }

  function setStringValue(v: string) {
    if (
      leaf.type === "EQUALS" ||
      leaf.type === "NOT_EQUALS" ||
      leaf.type === "CONTAINS" ||
      leaf.type === "MATCHES" ||
      leaf.type === "SEARCH"
    ) {
      rememberedValues.current.string = v;
      onChange({ ...leaf, value: v });
    }
  }

  function setNumberValue(v: number) {
    if (leaf.type === "LT" || leaf.type === "GT" || leaf.type === "LE" || leaf.type === "GE") {
      rememberedValues.current.number = v;
      onChange({ ...leaf, value: v });
    }
  }

  function setArrayValue(v: string) {
    if (leaf.type === "IN") {
      const arr = v
        .split(",")
        .map((s) => s.trim())
        .filter((s) => s.length > 0);
      rememberedValues.current.array = arr;
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
        {leaf.type === "SEARCH" ? null : (
          <>
            <Input
              value={fieldText}
              onChange={(e) => setField(e.target.value)}
              placeholder="field"
              list={fieldOptions.length > 0 ? "snooze-field-suggestions" : undefined}
            />
            {fieldOptions.length > 0 ? (
              <datalist id="snooze-field-suggestions">
                {fieldOptions.map((f) => (
                  <option key={f} value={f} />
                ))}
              </datalist>
            ) : null}
          </>
        )}
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
          leaf.type === "NOT_EQUALS" ||
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
        (leaf.type === "LT" || leaf.type === "GT" || leaf.type === "LE" || leaf.type === "GE") ? (
          <Input
            type="number"
            inputMode="decimal"
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
        <IconButton icon="plus" label="Add filter" variant="ghost" size="sm" onClick={fork} />
        {onDuplicate ? (
          <IconButton
            icon="copy"
            label="Duplicate"
            variant="ghost"
            size="sm"
            onClick={onDuplicate}
          />
        ) : null}
        <IconButton
          icon="trash"
          label="Remove"
          variant="ghostDanger"
          size="sm"
          onClick={handleRootOrEscalateDelete}
        />
      </div>
    </div>
  );
}
