// web/tests/e2e/global-setup.ts
import { execSync } from "node:child_process";
import { existsSync, mkdirSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));

// eslint-disable-next-line @typescript-eslint/require-await
export default async function globalSetup(): Promise<void> {
  const repoRoot = resolve(__dirname, "../../..");
  const webDir = resolve(repoRoot, "web");
  const binDir = resolve(webDir, "tests/e2e/.bin");
  const distDir = resolve(webDir, "dist");

  if (process.env.E2E_SKIP_BUILD === "1") return;

  mkdirSync(binDir, { recursive: true });

  if (!existsSync(resolve(distDir, "index.html"))) {
    execSync("npm run build", { cwd: webDir, stdio: "inherit" });
  }
  execSync(`go build -o ${binDir}/snooze-server ./cmd/snooze-server`, {
    cwd: repoRoot,
    stdio: "inherit",
  });
}
