import { defineConfig } from "vite";
import type { Plugin } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

// Preload the PRIMARY font file so a cold load does not have to wait for
// HTML -> CSS parse before discovering it. The fonts are self-hosted
// @fontsource packages imported in src/main.tsx; Vite extracts their CSS into
// the entry stylesheet, so the woff2 is otherwise only requested after the CSS
// is fetched and parsed. We preload ONLY the latin subset of the IBM Plex Sans
// VARIABLE font (the wght-normal asset), which covers the default UI text —
// not the mono weights, the italic variants, or the latin-ext/cyrillic/greek
// subsets. The asset is hash-stamped, so we read the real name from the build
// bundle in generateBundle rather than hardcoding it.
function preloadPrimaryFont(): Plugin {
  // Match e.g. assets/ibm-plex-sans-latin-wght-normal-<hash>.woff2.
  // The leading "-" after "normal" anchors the hash boundary so we never
  // accidentally match the latin-ext-wght-normal subset.
  const PRIMARY_FONT_RE = /ibm-plex-sans-latin-wght-normal-[\w-]+\.woff2$/;
  let primaryFontFile: string | undefined;
  let resolvedBase = "/";
  return {
    name: "snooze:preload-primary-font",
    apply: "build", // no-op in dev (transformIndexHtml is build-only anyway)
    configResolved(config) {
      // Read the configured base (e.g. /web/) so the preload href matches the
      // URLs Vite stamps onto every other asset.
      resolvedBase = config.base;
    },
    generateBundle(_options, bundle) {
      for (const fileName of Object.keys(bundle)) {
        if (PRIMARY_FONT_RE.test(fileName)) {
          primaryFontFile = fileName;
          break;
        }
      }
    },
    transformIndexHtml: {
      order: "post",
      handler() {
        if (!primaryFontFile) return; // not a real build, or font not emitted
        // resolvedBase already has a trailing slash (Vite normalizes it).
        const href = `${resolvedBase}${primaryFontFile}`;
        return [
          {
            tag: "link",
            attrs: {
              rel: "preload",
              as: "font",
              type: "font/woff2",
              crossorigin: "",
              href,
            },
            injectTo: "head",
          },
        ];
      },
    },
  };
}

export default defineConfig({
  base: "/web/",
  plugins: [react(), preloadPrimaryFont()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
    },
  },
  build: {
    outDir: "dist",
    sourcemap: true,
    target: "es2022",
    chunkSizeWarningLimit: 600,
    rollupOptions: {
      output: {
        manualChunks: (id) => {
          if (!id.includes("node_modules")) return undefined;
          // @tanstack packages — must be matched BEFORE the generic react check
          // because e.g. @tanstack/react-table includes "react" in its path.
          if (id.includes("@tanstack/react-table") || id.includes("@tanstack/table-core"))
            return "vendor-tanstack-table";
          if (id.includes("@tanstack/react-query") || id.includes("@tanstack/query-core"))
            return "vendor-tanstack-query";
          if (
            id.includes("@tanstack/react-router") ||
            id.includes("@tanstack/history") ||
            id.includes("@tanstack/store") ||
            id.includes("@tanstack/react-store")
          )
            return "vendor-tanstack-router";
          // Radix UI and its runtime dependencies (floating-ui, scroll-lock, etc.).
          // These must share a chunk with @radix-ui to avoid React circular deps.
          if (id.includes("@radix-ui")) return "vendor-radix";
          if (id.includes("@floating-ui")) return "vendor-radix";
          if (id.includes("react-remove-scroll")) return "vendor-radix";
          if (id.includes("react-style-singleton")) return "vendor-radix";
          if (id.includes("react-remove-scroll-bar")) return "vendor-radix";
          if (id.includes("use-callback-ref")) return "vendor-radix";
          if (id.includes("use-sidecar")) return "vendor-radix";
          if (id.includes("aria-hidden")) return "vendor-radix";
          // react-day-picker contains the substring "react", so — exactly like
          // the @tanstack/react-table matcher above — it MUST run BEFORE the
          // generic react check below or it would be swallowed into vendor-react.
          // date-fns (+ @date-fns/tz) is a transitive dep of react-day-picker
          // only — no direct date-fns imports exist in src/ — so co-locate it
          // here. These packages are reachable only from lazy route chunks
          // (editors/dashboard), keeping this chunk out of the eager preload set.
          if (
            id.includes("react-day-picker") ||
            id.includes("date-fns") ||
            id.includes("@date-fns")
          )
            return "vendor-daypicker";
          // @kurkle/color is chart.js's color dependency — keep it with chart.
          if (
            id.includes("chart.js") ||
            id.includes("react-chartjs-2") ||
            id.includes("@kurkle/color")
          )
            return "vendor-chart";
          // Lazy-only vendor splits — reachable only from lazy route chunks
          // (@dnd-kit <- rules tree; yaml <- dynamic imports + diff view; diff
          // <- rules diff view), so giving them their own chunks lets Rollup
          // drop them from the eager modulepreload set. Match "diff" precisely
          // via the path separator so we don't catch packages whose name merely
          // contains "diff".
          if (id.includes("@dnd-kit")) return "vendor-dndkit";
          if (id.includes("node_modules/yaml/")) return "vendor-yaml";
          if (id.includes("node_modules/diff/")) return "vendor-diff";
          if (id.includes("react-hook-form")) return "vendor-rhf";
          if (id.includes("zustand")) return "vendor-zustand";
          if (id.includes("jwt-decode")) return "vendor-jwt";
          // React ecosystem packages that must be co-located to avoid circular ESM
          // chunk dependencies (manifests as "Cannot set properties of undefined
          // (setting 'Children')" or "Cannot read properties of undefined (reading
          // 'useLayoutEffect')" at startup in Chromium headless mode).
          // scheduler and use-sync-external-store are tight runtime deps of react-dom.
          if (id.includes("use-sync-external-store")) return "vendor-react";
          if (id.includes("scheduler")) return "vendor-react";
          if (id.includes("react/") || id.includes("react-dom/")) return "vendor-react";
          return "vendor";
        },
      },
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": {
        target: "http://localhost:5200",
        changeOrigin: false,
      },
    },
  },
});
