import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import { TimeCell } from "@/shared/ui/TimeCell";
import { roleBadgeVariant } from "@/lib/format/role-color";
import type { User } from "./types";

// BadgeList renders a wrapping list of badges, capped at `max` with a "+N more"
// summary (full list in the title) so a user with many auth-backend groups
// doesn't blow up the row.
// eslint-disable-next-line react-refresh/only-export-components
function BadgeList({
  items,
  palette,
  max,
}: {
  items: string[] | undefined;
  palette?: (s: string) => string;
  max?: number;
}) {
  if (!items || items.length === 0) return <span style={{ color: "var(--text-muted)" }}>—</span>;
  const shown = max && items.length > max ? items.slice(0, max) : items;
  const extra = items.length - shown.length;
  return (
    <span
      style={{
        display: "inline-flex",
        gap: "var(--space-1)",
        flexWrap: "wrap",
        alignItems: "center",
      }}
    >
      {shown.map((v) => {
        const variant = palette ? palette(v) : "neutral";
        return (
          <Badge key={v} variant={variant as never}>
            {v}
          </Badge>
        );
      })}
      {extra > 0 ? (
        <span style={{ color: "var(--text-muted)" }} title={items.join(", ")}>
          +{extra} more
        </span>
      ) : null}
    </span>
  );
}

// RolesCell shows a user's *effective* roles: those explicitly assigned on the
// user document, plus any role whose Groups field matches one of the user's
// auth-backend groups (the group→role mapping). Group-derived roles are the
// reason an SSO admin has full access while carrying no explicit `roles` — they
// are shown here (with a tooltip) rather than appearing as an empty cell.
// eslint-disable-next-line react-refresh/only-export-components
function RolesCell({
  user,
  roleGroupIndex,
}: {
  user: User;
  roleGroupIndex: Map<string, string[]>;
}) {
  const explicit = user.roles ?? [];
  const explicitSet = new Set(explicit);
  const derived = new Set<string>();
  for (const g of user.groups ?? []) {
    for (const role of roleGroupIndex.get(g) ?? []) {
      if (!explicitSet.has(role)) derived.add(role);
    }
  }
  if (explicit.length === 0 && derived.size === 0)
    return <span style={{ color: "var(--text-muted)" }}>—</span>;
  return (
    <span style={{ display: "inline-flex", gap: "var(--space-1)", flexWrap: "wrap" }}>
      {explicit.map((r) => (
        <Badge key={r} variant={roleBadgeVariant(r) as never}>
          {r}
        </Badge>
      ))}
      {[...derived].map((r) => (
        <span
          key={r}
          title="Granted automatically via an auth-backend group (not directly editable)"
        >
          <Badge variant={roleBadgeVariant(r) as never}>{r}</Badge>
        </span>
      ))}
    </span>
  );
}

// makeUserColumns builds the user table columns. roleGroupIndex maps an
// auth-backend group value → the role names whose Groups field contains it, so
// the Roles column can surface group-derived (SSO/LDAP) roles in addition to
// the explicitly-assigned ones.
export function makeUserColumns(roleGroupIndex: Map<string, string[]>): ColumnDef<User>[] {
  return [
    {
      id: "name",
      header: "Name",
      cell: (r) => <Code>{r.name}</Code>,
      sortable: true,
      width: "180px",
    },
    {
      id: "type",
      header: "Type",
      cell: (r) => <Badge variant="neutral">{r.method ?? r.type ?? "local"}</Badge>,
      width: "90px",
    },
    {
      id: "enabled",
      header: "Status",
      // `enabled` absent means enabled (legacy/seeded docs); only an explicit
      // false marks a blocked account.
      cell: (r) =>
        r.enabled === false ? (
          <Badge variant="warning">Disabled</Badge>
        ) : (
          <Badge variant="ok">Enabled</Badge>
        ),
      width: "100px",
    },
    {
      id: "roles",
      header: "Roles",
      cell: (r) => <RolesCell user={r} roleGroupIndex={roleGroupIndex} />,
      width: "240px",
    },
    {
      id: "groups",
      header: "Groups",
      cell: (r) => <BadgeList items={r.groups} max={6} />,
      width: "220px",
    },
    {
      id: "last_login",
      header: "Last login",
      cell: (r) =>
        r.last_login ? (
          <TimeCell epoch={r.last_login} />
        ) : (
          <span style={{ color: "var(--text-muted)" }}>never</span>
        ),
      width: "120px",
    },
    {
      id: "comment",
      header: "Comment",
      cell: (r) => <span style={{ color: "var(--text-muted)" }}>{r.comment ?? "—"}</span>,
    },
  ];
}
