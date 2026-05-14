import { jwtDecode } from "jwt-decode";

export type JwtClaims = {
  sub?: string;
  exp?: number;
  iat?: number;
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
