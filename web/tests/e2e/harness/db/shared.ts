// web/tests/e2e/harness/db/shared.ts
//
// Helpers shared by the docker-backed launchers (mongo, postgres).
//
// Convention used here mirrors testcontainers / Playwright "one container per
// session" patterns:
//
//   * globalSetup starts a single container and writes its connection info
//     into web/tests/e2e/.bin/<driver>-conn.json. The container carries a
//     `snooze-e2e=1` label and a per-session label `snooze-e2e-session=<pid>`.
//   * Each Playwright worker reads that file in `perWorker()`. Workers do
//     NOT start their own containers; they pick a unique database name
//     (collection-name in mongo, schema/db in postgres) keyed by workerIndex.
//   * globalTeardown stops the container by reading the same file.
//   * Both globalSetup and globalTeardown sweep orphan containers with the
//     `snooze-e2e=1` label as a safety net (covers crashes/SIGKILLs).
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { readFile, writeFile, mkdir, rm } from "node:fs/promises";

const exec = promisify(execFile);

const here = dirname(fileURLToPath(import.meta.url));
export const binDir = resolve(here, "../../.bin");

export const SESSION_LABEL = "snooze-e2e=1";

export type ConnInfo = {
  containerId: string;
  containerName: string;
  hostPort: number;
  /** Full connection URI/DSN suitable for the snooze-server config. */
  uri: string;
};

export async function run(
  cmd: string,
  args: string[],
  timeoutMs = 10_000,
): Promise<string> {
  const { stdout } = await exec(cmd, args, { timeout: timeoutMs });
  return stdout.trim();
}

export async function runAllowFail(
  cmd: string,
  args: string[],
  timeoutMs = 10_000,
): Promise<string> {
  try {
    return await run(cmd, args, timeoutMs);
  } catch {
    return "";
  }
}

export function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

export async function poll<T>(
  fn: () => Promise<T | null | undefined | false>,
  deadlineMs: number,
  intervalMs = 1_000,
  label = "condition",
): Promise<T> {
  const end = Date.now() + deadlineMs;
  while (Date.now() < end) {
    const v = await fn().catch(() => null);
    if (v) return v as T;
    await sleep(intervalMs);
  }
  throw new Error(`Timed out waiting for: ${label}`);
}

export async function resolveHostPort(
  containerId: string,
  containerPort: number,
): Promise<number> {
  const out = await run("docker", ["port", containerId, `${containerPort}/tcp`]);
  const match = out.match(/:(\d+)/);
  if (!match || !match[1]) {
    throw new Error(`Cannot parse docker port for ${containerId}: ${out}`);
  }
  return parseInt(match[1], 10);
}

/**
 * Kill any container carrying the snooze-e2e label. Used as a safety net at
 * session start and end — covers orphans left behind by SIGKILLs / crashes.
 */
export async function sweepOrphans(): Promise<void> {
  const labelKey = SESSION_LABEL.split("=")[0] ?? "snooze-e2e";
  const ids = await runAllowFail("docker", [
    "ps",
    "-aq",
    "--filter",
    `label=${labelKey}`,
  ]);
  if (!ids) return;
  for (const id of ids.split("\n").filter(Boolean)) {
    await runAllowFail("docker", ["kill", id], 15_000);
    // --rm handles removal; rm is a belt-and-suspenders no-op on stopped ones.
    await runAllowFail("docker", ["rm", "-f", id], 15_000);
  }
}

export function connFile(driver: string): string {
  return resolve(binDir, `${driver}-conn.json`);
}

export async function writeConn(driver: string, info: ConnInfo): Promise<void> {
  await mkdir(binDir, { recursive: true });
  await writeFile(connFile(driver), JSON.stringify(info, null, 2), "utf-8");
}

export async function readConn(driver: string): Promise<ConnInfo> {
  const raw = await readFile(connFile(driver), "utf-8");
  return JSON.parse(raw) as ConnInfo;
}

export async function removeConn(driver: string): Promise<void> {
  await rm(connFile(driver), { force: true });
}
