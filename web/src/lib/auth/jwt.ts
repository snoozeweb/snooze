import { jwtDecode } from "jwt-decode";

export type JwtClaims = {
  sub?: string;
  exp?: number;
  iat?: number;
  /** Tenant slug stamped by the server (Shared Contract §4.1, Claims.TenantID).
   *  Empty or absent on legacy tokens; treat as "default" when missing. */
  tenant_id?: string;
  permissions?: string[];
  roles?: string[];
  [key: string]: unknown;
};

export function decodeJwt(token: string): JwtClaims | null {
  if (!token) return null;
  try {
    return jwtDecode<JwtClaims>(token);
  } catch {
    return null;
  }
}

export function isExpired(claims: JwtClaims, leewaySec = 0): boolean {
  if (claims.exp === undefined) return false;
  const nowSec = Math.floor(Date.now() / 1000);
  return claims.exp <= nowSec + leewaySec;
}

export function secondsUntilExpiry(claims: JwtClaims): number {
  if (claims.exp === undefined) return Infinity;
  const nowSec = Math.floor(Date.now() / 1000);
  return claims.exp - nowSec;
}

/** Returns the tenant slug from claims, falling back to "default" for legacy
 *  tokens that do not carry tenant_id (D10 / Shared Contract §4.1). */
export function tenantFromClaims(claims: JwtClaims | null): string {
  return claims?.tenant_id || "default";
}
