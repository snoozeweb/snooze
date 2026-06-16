import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import { TimeCell } from "@/shared/ui/TimeCell";
import { permissionBadgeVariant } from "@/lib/format/permission-color";
import type { ApiKey } from "./types";

// makeApiKeyColumns builds the admin API-key table columns. Mirrors the user /
// role column-def shape (id / header / cell / sortable / width). Keys are
// minted via self-service, so admins only inspect, edit name/expiry, and
// revoke — no create surface.
export function makeApiKeyColumns(): ColumnDef<ApiKey>[] {
  return [
    {
      id: "owner",
      header: "Owner",
      cell: (r) => <Code>{r.owner}</Code>,
      sortable: true,
      width: "180px",
    },
    {
      id: "name",
      header: "Name",
      cell: (r) => <Code>{r.name}</Code>,
      sortable: true,
      width: "180px",
    },
    {
      id: "key_prefix",
      header: "Prefix",
      cell: (r) =>
        r.key_prefix ? (
          <Code>{r.key_prefix}…</Code>
        ) : (
          <span style={{ color: "var(--text-muted)" }}>—</span>
        ),
      width: "160px",
    },
    {
      id: "permissions",
      header: "Permissions",
      cell: (r) => {
        const perms = r.permissions ?? [];
        if (perms.length === 0) return <span style={{ color: "var(--text-muted)" }}>—</span>;
        return (
          <span style={{ display: "inline-flex", gap: "var(--space-1)", flexWrap: "wrap" }}>
            {perms.map((p) => (
              <Badge key={p} variant={permissionBadgeVariant(p)}>
                {p}
              </Badge>
            ))}
          </span>
        );
      },
    },
    {
      id: "expires_at",
      header: "Expires",
      cell: (r) =>
        r.expires_at ? (
          <TimeCell epoch={r.expires_at} />
        ) : (
          <span style={{ color: "var(--text-muted)" }}>never</span>
        ),
      sortable: true,
      width: "160px",
    },
  ];
}
