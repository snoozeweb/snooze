import { decodeJwt, type JwtClaims } from "./jwt";

export const TOKEN_KEY = "snooze-token";
export const CLAIMS_KEY = "snooze-claims";

export function readToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY);
  } catch {
    return null;
  }
}

export function writeToken(token: string): void {
  try {
    localStorage.setItem(TOKEN_KEY, token);
    const claims = decodeJwt(token);
    if (claims) {
      localStorage.setItem(CLAIMS_KEY, JSON.stringify(claims));
    } else {
      localStorage.removeItem(CLAIMS_KEY);
    }
  } catch {
    // Quota exceeded, private mode, etc.
  }
}

export function clearToken(): void {
  try {
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(CLAIMS_KEY);
  } catch {
    // Best-effort.
  }
}

export function readClaims(): JwtClaims | null {
  try {
    const raw = localStorage.getItem(CLAIMS_KEY);
    if (!raw) return null;
    return JSON.parse(raw) as JwtClaims;
  } catch {
    return null;
  }
}
