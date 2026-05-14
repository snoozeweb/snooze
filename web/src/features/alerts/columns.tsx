import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import { formatRelativeTime, severityBadgeVariant, stateBadgeVariant, stateLabel } from "./format";
import type { AlertState, Record_ } from "./types";

export const alertColumns: ColumnDef<Record_>[] = [
  {
    id: "date_epoch",
    header: "When",
    cell: (r) => <span>{formatRelativeTime(r.date_epoch)}</span>,
    sortable: true,
    width: "80px",
  },
  {
    id: "severity",
    header: "Sev",
    cell: (r) => (
      <Badge variant={severityBadgeVariant(r.severity ?? "")}>{r.severity ?? "—"}</Badge>
    ),
    sortable: true,
    width: "100px",
  },
  {
    id: "state",
    header: "State",
    cell: (r) => {
      const state = (r.state ?? "") as AlertState;
      return <Badge variant={stateBadgeVariant(state)}>{stateLabel(state)}</Badge>;
    },
    sortable: true,
    width: "120px",
  },
  {
    id: "host",
    header: "Host",
    cell: (r) => <Code>{r.host ?? ""}</Code>,
    sortable: true,
    width: "160px",
  },
  {
    id: "source",
    header: "Source",
    cell: (r) => r.source ?? "—",
    width: "120px",
  },
  {
    id: "environment",
    header: "Environment",
    cell: (r) => r.environment ?? "—",
    width: "140px",
  },
  {
    id: "message",
    header: "Message",
    cell: (r) => (
      <span
        style={{
          display: "inline-block",
          maxWidth: "400px",
          overflow: "hidden",
          textOverflow: "ellipsis",
          whiteSpace: "nowrap",
        }}
      >
        {r.message ?? ""}
      </span>
    ),
  },
];
