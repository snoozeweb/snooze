// web/tests/e2e/harness/paths.ts
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
export const harnessDir = here;
export const e2eDir = resolve(here, "..");
export const webDir = resolve(e2eDir, "../..");
export const repoRoot = resolve(webDir, "..");
export const binPath = resolve(e2eDir, ".bin/snooze-server");
export const distDir = resolve(webDir, "dist");
