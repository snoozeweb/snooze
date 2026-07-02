import { useEffect } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Logo } from "@/shared/ui/Logo";
import { authStore } from "@/lib/auth/store";
import { isSafeInternalPath } from "@/lib/auth/return-to";
import { firstLandingPath } from "@/app/layout/nav-list";
import styles from "./Login.module.css";

// LoginCallback receives the OIDC redirect from the server. The server delivers
// the session in the URL fragment (never the query) so it does not hit server
// logs or the Referer header. We read it, store it, scrub the hash, and route on.
export function LoginCallback() {
  const navigate = useNavigate();

  useEffect(() => {
    const hash = window.location.hash.startsWith("#") ? window.location.hash.slice(1) : "";
    const params = new URLSearchParams(hash);
    const token = params.get("token");
    const refreshToken = params.get("refresh_token");
    const returnTo = params.get("return_to");

    if (!token) {
      void navigate({ to: "/web/login" });
      return;
    }
    authStore.getState().login(token, refreshToken);
    // Scrub the token from the address bar.
    window.history.replaceState(null, "", window.location.pathname);
    // `URLSearchParams` already decoded return_to once. Decoding again mangles a
    // deep link's still-encoded query state (e.g. "%3D" → "=") and throws on a
    // raw "%", silently discarding the destination — so use it directly, gated
    // to a safe same-origin path. Otherwise land on the user's first permitted
    // page (claims are set by login() above) rather than the record-gated
    // Alerts page, which would be an Access-denied wall for non-record users.
    const dest = isSafeInternalPath(returnTo)
      ? returnTo
      : firstLandingPath(authStore.getState().claims);
    void navigate({ to: dest });
  }, [navigate]);

  return (
    <div className={styles.page}>
      <div className={styles.card}>
        <div className={styles.brand}>
          <Logo />
        </div>
        <p className={styles.anonymous} role="status">
          Completing sign-in…
        </p>
      </div>
    </div>
  );
}
