import { useState } from "react";
import { Button } from "@/shared/ui/Button";
import { Spinner } from "@/shared/ui/Spinner";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from "@/shared/ui/Dialog";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import {
  useHousekeepingRun,
  useHousekeepingStatus,
  type HousekeepingRunResult,
} from "./housekeeping";
import styles from "./HousekeepingRunPanel.module.css";

/**
 * HousekeepingRunPanel is the admin-only trigger for the on-demand housekeeping
 * cycle (POST /housekeeping/run). It is a pure panel — the rw_all gate lives at
 * the call site (SettingsPage wraps it in <RequirePerm all={["rw_all"]}>) so a
 * settings-editor who lacks rw_all never sees a button the API would 403.
 *
 * The run is synchronous and destructive (cleanup jobs prune records past their
 * retention windows), so the trigger is behind a confirm dialog and the button
 * reflects the mutation's loading state. Per-job results are shown after a run.
 */
export function HousekeepingRunPanel() {
  const status = useHousekeepingStatus();
  const run = useHousekeepingRun();
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [lastResult, setLastResult] = useState<HousekeepingRunResult | null>(null);

  const notConfigured = status.error?.status === 503;
  const jobCount = status.data?.registered_jobs;

  async function doRun() {
    setConfirmOpen(false);
    try {
      const result = await run.mutateAsync();
      setLastResult(result);
      const errs = result.errors ?? 0;
      if (errs > 0) {
        toast.error(`Housekeeping finished with ${errs} job error${errs === 1 ? "" : "s"}.`);
      } else {
        const n = result.jobs?.length ?? 0;
        toast.success(`Housekeeping complete — ${n} job${n === 1 ? "" : "s"} run.`);
      }
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Housekeeping run failed");
    }
  }

  const disabled = status.isPending || notConfigured || run.isPending;

  return (
    <section className={styles.panel} aria-labelledby="hk-run-title">
      <div>
        <h3 id="hk-run-title" className={styles.title}>
          Run housekeeping now
        </h3>
        <p className={styles.subtitle}>
          Fires every registered cleanup job once, across all tenants. Jobs prune records past
          their configured retention windows, so this permanently deletes data.
        </p>
      </div>

      <div className={styles.statusRow}>
        {status.isPending ? (
          <span className={styles.muted}>
            <Spinner size={16} /> Checking housekeeper…
          </span>
        ) : notConfigured ? (
          <span className={styles.warn}>Housekeeper is not configured on this server.</span>
        ) : status.isError ? (
          <span className={styles.warn}>Couldn&rsquo;t read housekeeper status.</span>
        ) : (
          <span className={styles.muted}>
            {jobCount ?? 0} job{jobCount === 1 ? "" : "s"} registered.
          </span>
        )}
        <Button
          variant="primary"
          leadingIcon="rotate-cw"
          onClick={() => setConfirmOpen(true)}
          loading={run.isPending}
          disabled={disabled}
        >
          Run now
        </Button>
      </div>

      {lastResult ? <RunResults result={lastResult} /> : null}

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent>
          <DialogTitle>Run housekeeping?</DialogTitle>
          <DialogDescription>
            This runs every registered cleanup job immediately across all tenants and permanently
            deletes records that are past their configured retention windows. This can&rsquo;t be
            undone.
          </DialogDescription>
          <DialogFooter>
            <Button variant="secondary" onClick={() => setConfirmOpen(false)}>
              Cancel
            </Button>
            <Button variant="primary" leadingIcon="rotate-cw" onClick={() => void doRun()}>
              Run housekeeping
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

function RunResults({ result }: { result: HousekeepingRunResult }) {
  const jobs = result.jobs ?? [];
  if (jobs.length === 0) {
    return <div className={styles.results}>No jobs were run.</div>;
  }
  return (
    <div className={styles.results}>
      <span className={styles.resultsHead}>Last run</span>
      {jobs.map((job, i) => (
        <div key={job.name ?? i}>
          <div className={styles.resultRow}>
            <span className={styles.jobName}>{job.name ?? "(unnamed job)"}</span>
            {typeof job.duration_ms === "number" ? (
              <span className={styles.jobDuration}>{job.duration_ms} ms</span>
            ) : null}
          </div>
          {job.error ? <div className={styles.jobError}>{job.error}</div> : null}
        </div>
      ))}
    </div>
  );
}
