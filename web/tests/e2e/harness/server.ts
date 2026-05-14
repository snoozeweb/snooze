// web/tests/e2e/harness/server.ts
import { spawn, type ChildProcess } from "node:child_process";
import { rm } from "node:fs/promises";
import { binPath, distDir } from "./paths";
import { freePort } from "./ports";

export type ServerHandle = {
  baseURL: string;
  adminSocketPath: string;
  tmpdir: string;
  proc: ChildProcess;
  stop(): Promise<void>;
};

export type SpawnOptions = {
  tmpdir: string;
  env: Record<string, string>;
  extraFlags?: string[];
};

export async function startServer(opts: SpawnOptions): Promise<ServerHandle> {
  const port = await freePort();
  const baseURL = `http://127.0.0.1:${port}`;
  const adminSocketPath = `${opts.tmpdir}/admin.sock`;

  const args = [
    "-config", `${opts.tmpdir}/cfg`,
    "-listen", `127.0.0.1:${port}`,
    "-admin-socket", adminSocketPath,
    "-web-dir", distDir,
    "-log-level", "warn",
    ...(opts.extraFlags ?? []),
  ];

  const proc = spawn(binPath, args, {
    cwd: opts.tmpdir,
    env: { ...process.env, ...opts.env },
    stdio: ["ignore", "pipe", "pipe"],
  });

  proc.stderr?.on("data", (b: Buffer) => {
    if (process.env.E2E_LOG_SERVER === "1") {
      process.stderr.write(`[snooze ${port}] ${b.toString()}`);
    }
  });

  // Wait for /healthz to come up. Budget: 15s.
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${baseURL}/healthz`);
      if (res.ok) {
        return {
          baseURL,
          adminSocketPath,
          tmpdir: opts.tmpdir,
          proc,
          async stop() {
            proc.kill("SIGTERM");
            await new Promise<void>((r) => proc.once("exit", () => r()));
            await rm(opts.tmpdir, { recursive: true, force: true });
          },
        };
      }
    } catch {
      // not up yet
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  proc.kill("SIGKILL");
  throw new Error(`snooze-server did not become healthy on ${baseURL}`);
}
