import type { JwtClaims } from "./jwt";

// Backend wildcard permission granted to the root user and admins.
// Mirrors internal/auth.AllPermission ("rw_all"). Holding this permission
// satisfies any required permission check, just like on the server.
const WILDCARD_PERMISSION = "rw_all";

// Platform-tier permissions (Shared Contract §4.3, D5). These are evaluated
// against platform scope — they gate /api/v1/tenant registry CRUD and are
// independent of any tenant. Mirrored from internal/auth.PermReadTenant and
// PermWriteTenant.
const PLATFORM_PERMISSIONS = new Set(["ro_tenant", "rw_tenant"]);

/** Returns true when p is a platform-tier permission (ro_tenant / rw_tenant). */
export function isPlatformPermission(p: string): boolean {
  return PLATFORM_PERMISSIONS.has(p);
}

function getPerms(claims: JwtClaims | null): readonly string[] {
  if (!claims) return [];
  return Array.isArray(claims.permissions) ? claims.permissions : [];
}

function hasWildcard(perms: readonly string[]): boolean {
  return perms.includes(WILDCARD_PERMISSION);
}

export function hasPermission(claims: JwtClaims | null, permission: string): boolean {
  const perms = getPerms(claims);
  if (hasWildcard(perms)) return true;
  return perms.includes(permission);
}

export function hasAnyPermission(
  claims: JwtClaims | null,
  permissions: readonly string[],
): boolean {
  if (permissions.length === 0) return false;
  const perms = getPerms(claims);
  if (hasWildcard(perms)) return true;
  return permissions.some((p) => perms.includes(p));
}

export function hasAllPermissions(
  claims: JwtClaims | null,
  permissions: readonly string[],
): boolean {
  if (permissions.length === 0) return true;
  const perms = getPerms(claims);
  if (hasWildcard(perms)) return true;
  return permissions.every((p) => perms.includes(p));
}
