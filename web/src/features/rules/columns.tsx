import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import { prettyCondition } from "@/lib/condition/pretty";
import { secondsToHuman } from "@/lib/format/seconds";
import type { AggregateRule, Rule } from "./types";

// eslint-disable-next-line react-refresh/only-export-components
function ConditionCell({ rule }: { rule: Rule }) {
  return (
    <span style={{ fontFamily: "var(--font-mono)", fontSize: "var(--text-xs)" }}>
      {prettyCondition(rule.condition)}
    </span>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
function ModificationsCell({ mods }: { mods: unknown[][] | undefined }) {
  if (!mods || mods.length === 0)
    return <span style={{ color: "var(--text-muted)" }}>—</span>;
  return (
    <span style={{ display: "inline-flex", gap: "var(--space-1)", flexWrap: "wrap" }}>
      {mods.map((m, i) => {
        const op = String((m[0] as string | number | null | undefined) ?? "");
        const field = String((m[1] as string | number | null | undefined) ?? "");
        return (
          <Badge key={i} variant="neutral">
            {op} {field}
          </Badge>
        );
      })}
    </span>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
function StringListCell({ items }: { items: string[] | undefined }) {
  if (!items || items.length === 0)
    return <span style={{ color: "var(--text-muted)" }}>—</span>;
  return (
    <span style={{ display: "inline-flex", gap: "var(--space-1)", flexWrap: "wrap" }}>
      {items.map((v) => (
        <Badge key={v} variant="muted">
          {v}
        </Badge>
      ))}
    </span>
  );
}

// Rule columns: name + pretty condition + modifications. Drop "enabled"
// (replaced by row-level greying via rowDisabled).
export const ruleColumns: ColumnDef<Rule>[] = [
  {
    id: "name",
    header: "Name",
    cell: (r) => <Code>{r.name}</Code>,
    sortable: true,
    width: "220px",
  },
  {
    id: "condition",
    header: "Condition",
    cell: (r) => <ConditionCell rule={r} />,
  },
  {
    id: "modifications",
    header: "Modifications",
    cell: (r) => <ModificationsCell mods={r.modifications} />,
    width: "260px",
  },
];

// Aggregate-rule columns: name + condition + fields + watch + throttle.
export const aggregateRuleColumns: ColumnDef<AggregateRule>[] = [
  {
    id: "name",
    header: "Name",
    cell: (r) => <Code>{r.name}</Code>,
    sortable: true,
    width: "200px",
  },
  {
    id: "condition",
    header: "Condition",
    cell: (r) => <ConditionCell rule={r} />,
  },
  {
    id: "fields",
    header: "Fields",
    cell: (r) => <StringListCell items={r.fields} />,
    width: "180px",
  },
  {
    id: "watch",
    header: "Watch",
    cell: (r) => <StringListCell items={r.watch} />,
    width: "180px",
  },
  {
    id: "throttle",
    header: "Throttle",
    cell: (r) => {
      const t = r.throttle;
      if (t === undefined || t === null) return <Badge variant="neutral">—</Badge>;
      if (typeof t === "number") {
        return <Badge variant={t ? "muted" : "neutral"}>{t ? secondsToHuman(t) : "—"}</Badge>;
      }
      const overrides = Object.keys(t).filter((k) => k !== "default").length;
      const def = typeof t.default === "number" ? secondsToHuman(t.default) : "—";
      return <Badge variant="muted">{`${overrides} override${overrides === 1 ? "" : "s"} · default ${def}`}</Badge>;
    },
    width: "160px",
  },
];

