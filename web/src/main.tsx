import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider } from "@tanstack/react-router";
import { router } from "@/app/router";
import { fetchConsoleConfig } from "@/features/config/api";
import { setSeverityRanks } from "@/lib/format/severity-color";
import "@fontsource-variable/ibm-plex-sans";
import "@fontsource/ibm-plex-mono/400.css";
import "@fontsource/ibm-plex-mono/500.css";
import "@fontsource/ibm-plex-mono/600.css";
import "@/styles/base.css";

// Prime the severity-rank ladder from the server config (the Plan 21 ladder is
// the runtime source of truth). Fire-and-forget so it never blocks first
// paint; until it resolves, severity-color's built-in RANK map is the offline
// fallback. The public /api/v1/config endpoint needs no auth token, so this is
// safe to run before login. fetchConsoleConfig swallows errors and returns the
// hardcode fallback (empty severity_ranks ⇒ a no-op set), so this never throws.
void fetchConsoleConfig().then((cfg) => setSeverityRanks(cfg.severity_ranks));

const rootElement = document.getElementById("root");
if (!rootElement) {
  throw new Error("Missing #root element in index.html");
}

createRoot(rootElement).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>,
);
