import { useEffect } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Logo } from "@/shared/ui/Logo";
import { authStore } from "@/lib/auth/store";
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
    let dest = "/web/alerts";
    if (returnTo) {
      try {
        dest = decodeURIComponent(returnTo);
      } catch {
        dest = "/web/alerts";
      }
    }
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
