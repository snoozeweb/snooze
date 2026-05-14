import { create } from "zustand";
import { decodeJwt, isExpired, type JwtClaims } from "./jwt";
import { clearToken, readClaims, readToken, writeToken } from "./storage";

export type AuthState = {
  token: string | null;
  claims: JwtClaims | null;
  isAuthenticated: boolean;
  login: (token: string) => void;
  logout: () => void;
  refresh: () => void;
};

function buildSnapshot(token: string | null, claims: JwtClaims | null) {
  const isAuthenticated = !!token && !!claims && !isExpired(claims);
  return { token, claims, isAuthenticated };
}

function readStorageSnapshot(): { token: string | null; claims: JwtClaims | null } {
  const token = readToken();
  if (!token) return { token: null, claims: null };
  // Prefer the cached claims; fall back to decoding the token directly
  // (handles the case where another tab wrote only snooze-token).
  const claims = readClaims() ?? decodeJwt(token);
  return { token, claims };
}

export const authStore = create<AuthState>((set) => {
  const { token, claims } = readStorageSnapshot();
  return {
    ...buildSnapshot(token, claims),
    login: (token: string) => {
      writeToken(token);
      set(buildSnapshot(token, readClaims()));
    },
    logout: () => {
      clearToken();
      set(buildSnapshot(null, null));
    },
    refresh: () => {
      const { token, claims } = readStorageSnapshot();
      set(buildSnapshot(token, claims));
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
