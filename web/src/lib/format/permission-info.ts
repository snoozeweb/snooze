// Human-readable descriptions for the RBAC permission strings, so an operator
// building a role sees what each grant actually does rather than having to
// decode the codebase's `ro_/rw_/can_` naming convention. Colour alone (see
// permission-color.ts) conveys rough severity; this conveys effect.

const RESOURCE_LABELS: Record<string, string> = {
  all: "every resource",
  alert: "alerts",
  record: "alerts",
  rule: "rules",
  aggregaterule: "aggregate rules",
  savedsearch: "saved searches",
  // `notification` and `action` are separate plugins with independent grants —
  // don't bundle them into one description.
  notification: "notifications",
  action: "actions",
  user: "users",
  tenant: "tenants (organisations)",
  audit: "the audit trail",
  notificationlog: "the notification delivery history",
  stats: "statistics and metrics",
  secret: "stored secrets (credentials)",
};

// Cases the prefix rules can't phrase well on their own.
const SPECIAL: Record<string, string> = {
  rw_all: "Full read/write access to every resource (effectively a superuser).",
  ro_all: "Read-only access to every resource.",
  can_comment: "Comment on alerts and change their state (acknowledge / close / re-open).",
  can_escalate: "Escalate alerts to the next level.",
  can_snooze: "Create and manage snooze windows.",
};

/**
 * permissionDescription returns a one-line explanation of what a permission
 * grants, or undefined when the string is unrecognised and has no known prefix
 * (so callers can skip a useless tooltip rather than echo the raw token).
 */
export function permissionDescription(perm: string): string | undefined {
  if (SPECIAL[perm]) return SPECIAL[perm];
  const rw = /^(ro|rw)_(.+)$/.exec(perm);
  if (rw) {
    const label = RESOURCE_LABELS[rw[2] as string] ?? (rw[2] as string).replace(/_/g, " ");
    return rw[1] === "rw" ? `Read and write ${label}.` : `Read-only access to ${label}.`;
  }
  const can = /^can_(.+)$/.exec(perm);
  if (can) return `Allowed to ${(can[1] as string).replace(/_/g, " ")}.`;
  return undefined;
}
