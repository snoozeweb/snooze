import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Icon } from "@/shared/icons/Icon";
import { TimeCell } from "@/shared/ui/TimeCell";
import { docsUrl } from "@/lib/docs";
import type { InputRow } from "./types";

const FAMILY_LABEL: Record<string, string> = {
  rest: "REST",
  webhook: "Webhook",
  daemon: "Daemon",
  other: "Other",
};

const muted = { color: "var(--text-muted)" } as const;

/**
 * makeInputColumns builds the Inputs table columns. `onSetup` fires when the
 * row's Setup button is clicked, carrying the input's catalogue id — the page
 * turns that into a `?setup=<id>` deep link that pre-focuses InjectAlertsDialog.
 */
export function makeInputColumns(onSetup: (id: string) => void): ColumnDef<InputRow>[] {
  return [
    { id: "name", header: "Input", cell: (r) => <span>{r.name}</span>, width: "200px" },
    {
      id: "family",
      header: "Type",
      cell: (r) => (
        <Badge variant={r.family === "other" ? "muted" : "neutral"}>
          {FAMILY_LABEL[r.family] ?? r.family}
        </Badge>
      ),
      width: "120px",
    },
    {
      id: "last",
      header: "Last received",
      cell: (r) =>
        r.restNoActivity ? (
          <span
            style={muted}
            title="REST alerts carry a caller-defined source, so they aren't attributed to this row."
          >
            —
          </span>
        ) : r.lastEpoch ? (
          <TimeCell epoch={r.lastEpoch} />
        ) : (
          // Not "never": activity is only looked back 30 days, so an input that
          // last fired 45 days ago (or a decommissioned one) would otherwise
          // look identical to one that was never configured.
          <span
            style={muted}
            title="No alerts received in the last 30 days — the activity lookback window."
          >
            none in 30d
          </span>
        ),
      width: "160px",
    },
    {
      id: "count",
      header: "Alerts (30d)",
      align: "right",
      cell: (r) => (r.count !== undefined ? <span>{r.count}</span> : <span style={muted}>—</span>),
      width: "100px",
    },
    {
      id: "docs",
      header: "Docs",
      cell: (r) =>
        r.docSlug ? (
          <a href={docsUrl(r.docSlug)} target="_blank" rel="noreferrer">
            <Icon name="book" size={14} /> Docs <span aria-hidden="true">↗</span>
          </a>
        ) : (
          <span style={muted}>—</span>
        ),
      width: "120px",
    },
    {
      id: "setup",
      header: "Setup",
      align: "right",
      cell: (r) =>
        r.catalogue ? (
          <Button size="sm" variant="ghost" leadingIcon="download" onClick={() => onSetup(r.id)}>
            Setup
          </Button>
        ) : null,
      width: "120px",
    },
  ];
}
