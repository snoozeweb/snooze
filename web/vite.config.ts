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
          if (id.includes("@tanstack/react-table")) return "vendor-tanstack-table";
          if (id.includes("@tanstack/react-query")) return "vendor-tanstack-query";
          if (id.includes("@tanstack/react-router")) return "vendor-tanstack-router";
          if (id.includes("@radix-ui")) return "vendor-radix";
          if (id.includes("chart.js") || id.includes("react-chartjs-2")) return "vendor-chart";
          if (id.includes("react-hook-form")) return "vendor-rhf";
          if (id.includes("zustand")) return "vendor-zustand";
          if (id.includes("jwt-decode")) return "vendor-jwt";
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
