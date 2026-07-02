// Confirm-dialog copy for role deletion. The generic "delete the selected role"
// message gives no sense of blast radius: deleting a role in active use strips
// its permissions from every assigned user the instant it succeeds.
export type RoleLike = { name?: string };

export function describeRoleDelete(rows: RoleLike[]): { title?: string; message: string } {
  if (rows.length === 1) {
    const name = rows[0]?.name || "this role";
    return {
      title: `Delete role ${name}?`,
      message: `Any users assigned "${name}" (directly or via group mapping) immediately lose the permissions it grants. This cannot be undone.`,
    };
  }
  return {
    title: `Delete ${rows.length} roles?`,
    message: `Users assigned these roles immediately lose the permissions they grant. This cannot be undone.`,
  };
}
