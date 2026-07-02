// Confirm-dialog copy for tenant deletion — the single most destructive admin
// action (an entire org's alerts, users, and config become inaccessible). The
// list-level delete previously showed the generic "delete the selected tenant"
// message, weaker than the editor's own danger-zone wording; this matches it.
export type TenantLike = { display_name?: string; id?: string };

export function describeTenantDelete(rows: TenantLike[]): { title?: string; message: string } {
  if (rows.length === 1) {
    const label = rows[0]?.display_name || rows[0]?.id || "this tenant";
    return {
      title: `Delete tenant ${label}?`,
      message: `All of ${label}'s alerts, users, and configuration become inaccessible. This action cannot be undone.`,
    };
  }
  return {
    title: `Delete ${rows.length} tenants?`,
    message: `All data for these ${rows.length} tenants becomes inaccessible. This action cannot be undone.`,
  };
}
