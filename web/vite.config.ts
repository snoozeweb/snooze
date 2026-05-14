import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

export default defineConfig({
  base: "/web/",
  plugins: [react()],
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
          if (id.includes("@tanstack/react-table") || id.includes("@tanstack/table-core")) return "vendor-tanstack-table";
          if (
            id.includes("@tanstack/react-query") ||
            id.includes("@tanstack/query-core")
          ) return "vendor-tanstack-query";
          if (
            id.includes("@tanstack/react-router") ||
            id.includes("@tanstack/history") ||
            id.includes("@tanstack/store") ||
            id.includes("@tanstack/react-store")
          ) return "vendor-tanstack-router";
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
          if (id.includes("chart.js") || id.includes("react-chartjs-2")) return "vendor-chart";
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
