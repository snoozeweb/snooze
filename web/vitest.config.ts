import { defineConfig, mergeConfig } from "vitest/config";
import viteConfig from "./vite.config";

export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      environment: "jsdom",
      setupFiles: ["./src/tests/setup.ts"],
      globals: true,
      css: false,
      // Vitest's default file pattern includes *.spec.ts, which would pull
      // in the Playwright suite under tests/e2e/. Pin Vitest to src/ only.
      include: ["src/**/*.{test,spec}.{ts,tsx}"],
      coverage: {
        provider: "v8",
        reporter: ["text", "html", "lcov"],
        include: ["src/**/*.{ts,tsx}"],
        exclude: ["src/**/*.test.{ts,tsx}", "src/tests/**"],
      },
    },
  }),
);
