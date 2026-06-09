import type { JwtClaims } from "./jwt";
import { tenantFromClaims } from "./jwt";

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

// Reserved slug hosting the platform tier. Mirrors snoozetypes.DefaultTenant and
// jwt.tenantFromClaims' legacy fallback.
const DEFAULT_TENANT = "default";

/**
 * hasPlatformPermission gates platform-tier UI (the tenant registry) the same
 * way the backend's RequirePlatformPerm (internal/api/middleware/permission.go)
 * gates /api/v1/tenant. Unlike hasAnyPermission it is deliberately strict on two
 * axes, so the menu never offers a route whose API would 403 the caller:
 *
 *   - Platform origin: the caller must be authenticated against the default
 *     tenant (platform admins live there, D5).
 *   - Literal membership: one of `permissions` must be held verbatim. The
 *     rw_all wildcard does NOT satisfy a platform perm.
 */
export function hasPlatformPermission(
  claims: JwtClaims | null,
  permissions: readonly string[],
): boolean {
  if (permissions.length === 0) return false;
  if (tenantFromClaims(claims) !== DEFAULT_TENANT) return false;
  const perms = getPerms(claims);
  return permissions.some((p) => perms.includes(p));
}
