import type { ColumnDef } from "@/shared/ui/DataTable";
import { Code } from "@/shared/ui/Code";
import { encodeText } from "@/lib/condition/text";
import type { ForwardDestination } from "./types";

export function formatAuthType(t: string | undefined): string {
  switch (t) {
    case "bearer":
      return "Bearer token";
    case "basic":
      return "Basic";
    case "apikey":
      return "API key";
    default:
      return "none";
  }
}

export const forwardColumns: ColumnDef<ForwardDestination>[] = [
  {
    id: "name",
    header: "Name",
    cell: (r) => <Code>{r.name}</Code>,
    sortable: true,
    width: "200px",
  },
  {
    id: "enabled",
    header: "Enabled",
    sortable: true,
    width: "100px",
    cell: (r) =>
      r.enabled === false ? (
        <span style={{ color: "var(--text-muted)" }}>Disabled</span>
      ) : (
        <span style={{ color: "var(--color-success, green)" }}>Enabled</span>
      ),
  },
  {
    id: "endpoint",
    header: "Endpoint",
    cell: (r) => {
      const ep = r.endpoint ?? "";
      return ep.length > 60 ? (
        <span title={ep}>{ep.slice(0, 60)}…</span>
      ) : (
        <span>{ep || <span style={{ color: "var(--text-muted)" }}>—</span>}</span>
      );
    },
  },
  {
    id: "auth",
    header: "Auth",
    width: "120px",
    cell: (r) => <Code>{formatAuthType(r.auth?.type)}</Code>,
  },
  {
    id: "condition",
    header: "Condition",
    cell: (r) => {
      if (!r.condition || r.condition.type === "ALWAYS_TRUE") {
        return <span style={{ color: "var(--text-muted)" }}>—</span>;
      }
      try {
        return <Code>{encodeText(r.condition)}</Code>;
      } catch {
        return <span style={{ color: "var(--text-muted)" }}>(invalid)</span>;
      }
    },
  },
  {
    id: "event_classes",
    header: "Event classes",
    cell: (r) => (
      <span>
        {r.event_classes?.join(", ") ?? <span style={{ color: "var(--text-muted)" }}>—</span>}
      </span>
    ),
  },
];
