import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import { secondsToHuman } from "@/lib/format/seconds";
import { formatRelativeTime } from "@/lib/format/time";
import { STATUS_BADGE } from "./statusBadge";
import { parsedLastSeenEpoch } from "./format";
import type { Heartbeat } from "./types";

export const heartbeatColumns: ColumnDef<Heartbeat>[] = [
  {
    // No `width`: every other column here is fixed, so Name flexes to
    // absorb whatever space is left under fixed table layout.
    id: "name",
    header: "Name",
    cell: (r) => <Code>{r.name}</Code>,
    sortable: true,
  },
  {
    id: "status",
    header: "Status",
    cell: (r) => {
      const s = r.status ?? "ok";
      return <Badge variant={STATUS_BADGE[s]}>{s}</Badge>;
    },
    width: "110px",
  },
  {
    id: "interval",
    header: "Interval",
    cell: (r) => (
      <span>
        {secondsToHuman(r.interval)}
        {r.grace ? ` + ${secondsToHuman(r.grace)} grace` : ""}
      </span>
    ),
    width: "180px",
    hideBelow: "lg",
  },
  {
    id: "last_seen",
    header: "Last seen",
    cell: (r) => {
      const epochSec = parsedLastSeenEpoch(r.last_seen);
      if (epochSec === undefined) return <span style={{ color: "var(--text-muted)" }}>never</span>;
      return <span title={r.last_seen}>{formatRelativeTime(epochSec)}</span>;
    },
    sortable: true,
    width: "130px",
  },
  {
    id: "latency",
    header: "Latency",
    cell: (r) => {
      if (r.last_latency === undefined) {
        return <span style={{ color: "var(--text-muted)" }}>—</span>;
      }
      const max = r.max_latency;
      return (
        <span>
          {r.last_latency} ms{max ? ` / ${max} ms` : ""}
        </span>
      );
    },
    width: "160px",
    hideBelow: "xl",
  },
];
