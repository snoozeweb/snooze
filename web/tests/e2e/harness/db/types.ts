// web/tests/e2e/harness/db/types.ts
export type WorkerDbConfig = {
  tmpdir: string;
  env: Record<string, string>;
  extraFlags?: string[];
};

export interface DbLauncher {
  driver: "sqlite" | "postgres" | "mongo";
  perWorker(workerIndex: number): Promise<WorkerDbConfig>;
  teardown(worker: WorkerDbConfig): Promise<void>;
}
