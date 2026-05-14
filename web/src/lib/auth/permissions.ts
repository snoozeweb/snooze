import type { JwtClaims } from "./jwt";

function getPerms(claims: JwtClaims | null): readonly string[] {
  if (!claims) return [];
  return Array.isArray(claims.permissions) ? claims.permissions : [];
}

export function hasPermission(claims: JwtClaims | null, permission: string): boolean {
  return getPerms(claims).includes(permission);
}

export function hasAnyPermission(
  claims: JwtClaims | null,
  permissions: readonly string[],
): boolean {
  if (permissions.length === 0) return false;
  const perms = getPerms(claims);
  return permissions.some((p) => perms.includes(p));
}

export function hasAllPermissions(
  claims: JwtClaims | null,
  permissions: readonly string[],
): boolean {
  if (permissions.length === 0) return true;
  const perms = getPerms(claims);
  return permissions.every((p) => perms.includes(p));
}
