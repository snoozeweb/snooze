import type { JwtClaims } from "./jwt";
import { tenantFromClaims } from "./jwt";

// Backend wildcard permission granted to the root user and admins.
// Mirrors internal/auth.AllPermission ("rw_all"). Holding this permission
// satisfies any required permission check, just like on the server.
const WILDCARD_PERMISSION = "rw_all";

// The READ wildcard. internal/plugins/authz.go builds its read set as
// {ro_all, rw_all, ro_<plugin>, rw_<plugin>}, so `ro_all` grants every
// `ro_*` read on the server — and only those: it is absent from the write
// set. The SPA has to agree, or a read-everything identity (the analysis
// agent holds `ro_all` + `rw_protected`) is bounced off the Alerts page by a
// gate the API would have let through.
const READ_WILDCARD_PERMISSION = "ro_all";
const READ_PREFIX = "ro_";

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

/**
 * satisfies answers "does this permission set cover `required`?" the way the
 * server's read/write sets do:
 *
 *   - the literal permission always counts;
 *   - `rw_all` covers everything (internal/auth.AllPermission);
 *   - `ro_all` covers a `ro_*` read and NOTHING else — it is a read wildcard,
 *     not an admin one, so `ro_all` never satisfies a `rw_*` requirement.
 *
 * Note this is the generic CRUD gate. Routes the server checks LITERALLY —
 * the platform tier (hasPlatformPermission below) and the protected-field
 * write (`features/alerts/analysis/perms.ts`) — deliberately do not come
 * through here.
 */
function satisfies(perms: readonly string[], required: string): boolean {
  if (perms.includes(required)) return true;
  if (hasWildcard(perms)) return true;
  return required.startsWith(READ_PREFIX) && perms.includes(READ_WILDCARD_PERMISSION);
}

export function hasPermission(claims: JwtClaims | null, permission: string): boolean {
  return satisfies(getPerms(claims), permission);
}

export function hasAnyPermission(
  claims: JwtClaims | null,
  permissions: readonly string[],
): boolean {
  if (permissions.length === 0) return false;
  const perms = getPerms(claims);
  return permissions.some((p) => satisfies(perms, p));
}

export function hasAllPermissions(
  claims: JwtClaims | null,
  permissions: readonly string[],
): boolean {
  if (permissions.length === 0) return true;
  const perms = getPerms(claims);
  return permissions.every((p) => satisfies(perms, p));
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
