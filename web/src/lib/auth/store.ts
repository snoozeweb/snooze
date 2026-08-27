import { create } from "zustand";
import { decodeJwt, isExpired, type JwtClaims } from "./jwt";
import { revokeStoredRefreshToken } from "./revoke";
import {
  clearToken,
  readClaims,
  readRefreshToken,
  readToken,
  writeRefreshToken,
  writeToken,
} from "./storage";

export type AuthState = {
  token: string | null;
  claims: JwtClaims | null;
  refreshToken: string | null;
  isAuthenticated: boolean;
  login: (token: string, refreshToken?: string | null) => void;
  /**
   * Ends the session. By default the stored refresh token is also revoked
   * server-side — clearing localStorage alone leaves it usable for the rest of
   * its lease. Pass `{ revoke: false }` only when the token is already known to
   * be dead (the server just rejected it), to save a pointless round trip.
   */
  logout: (opts?: { revoke?: boolean }) => void;
  refresh: () => void;
};

function buildSnapshot(
  token: string | null,
  claims: JwtClaims | null,
  refreshToken: string | null,
) {
  const isAuthenticated = !!token && !!claims && !isExpired(claims);
  return { token, claims, refreshToken, isAuthenticated };
}

function readStorageSnapshot(): {
  token: string | null;
  claims: JwtClaims | null;
  refreshToken: string | null;
} {
  const refreshToken = readRefreshToken();
  const token = readToken();
  if (!token) return { token: null, claims: null, refreshToken };
  // Prefer the cached claims; fall back to decoding the token directly
  // (handles the case where another tab wrote only snooze-token).
  const claims = readClaims() ?? decodeJwt(token);
  return { token, claims, refreshToken };
}

export const authStore = create<AuthState>((set) => {
  const { token, claims, refreshToken } = readStorageSnapshot();
  return {
    ...buildSnapshot(token, claims, refreshToken),
    login: (token: string, refreshToken?: string | null) => {
      writeToken(token);
      if (refreshToken !== undefined) {
        writeRefreshToken(refreshToken);
      }
      set(buildSnapshot(token, readClaims(), readRefreshToken()));
    },
    logout: (opts?: { revoke?: boolean }) => {
      // Revoke first: clearToken() drops the very token the request needs.
      // The call is fire-and-forget, so this costs nothing observable.
      if (opts?.revoke !== false) revokeStoredRefreshToken();
      clearToken();
      set(buildSnapshot(null, null, null));
    },
    refresh: () => {
      const { token, claims, refreshToken } = readStorageSnapshot();
      set(buildSnapshot(token, claims, refreshToken));
    },
  };
});

export function useAuth(): AuthState {
  return authStore((s) => s);
}

// Cross-tab sync: when another tab writes/clears snooze-token, mirror
// the change here. The "storage" event only fires on changes made in
// *other* documents; same-doc writes go through writeToken/clearToken.
if (typeof window !== "undefined") {
  window.addEventListener("storage", (e) => {
    if (e.key === "snooze-token") {
      authStore.getState().refresh();
    }
  });
}
