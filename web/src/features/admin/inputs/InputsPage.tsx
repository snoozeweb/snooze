import { useCallback, useMemo } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { DataTable } from "@/shared/ui/DataTable";
import { Button } from "@/shared/ui/Button";
import { Card } from "@/shared/ui/Card";
import { EmptyState } from "@/shared/ui/EmptyState";
import { INJECTION_SOURCES } from "@/features/alerts/injectionGuide";
import { InjectAlertsDialog } from "@/features/alerts/InjectAlertsDialog";
import { useInputActivity } from "./api";
import { mergeCatalogueWithActivity } from "./merge";
import { makeInputColumns } from "./columns";
import type { InputRow } from "./types";
import styles from "./InputsPage.module.css";

type InputsSearch = { setup?: string };

// TanStack Router's navigate types are locked to the registered route tree at
// build time (route wiring is Task 7, not this one) — cast through unknown
// like useResourceListPage's updateSearch idiom.
type NavigateFn = (opts: {
  to: string;
  search: (prev: InputsSearch | undefined) => InputsSearch;
}) => Promise<void>;

export function InputsPage() {
  const search = useSearch({ strict: false }) as unknown as InputsSearch;
  const navigate = useNavigate();
  const query = useInputActivity();

  // Writes `?setup=<id>` (or clears it for `undefined`). The header button
  // opens with `id: ""` (no pre-selection, defaults to the REST tab);
  // row Setup buttons pass their catalogue id; closing the dialog clears it.
  const setSetup = useCallback(
    (id: string | undefined) => {
      void (navigate as unknown as NavigateFn)({
        to: "/web/admin/inputs",
        search: (prev) => {
          const merged: Record<string, unknown> = { ...(prev ?? {}) };
          if (id === undefined) delete merged["setup"];
          else merged["setup"] = id;
          return merged as InputsSearch;
        },
      });
    },
    [navigate],
  );

  const { inputs, other } = useMemo(() => {
    const merged = mergeCatalogueWithActivity(INJECTION_SOURCES, query.data?.data ?? []);
    const byCountDesc = (a: InputRow, b: InputRow) => (b.count ?? 0) - (a.count ?? 0);
    return {
      inputs: [...merged.inputs].sort(byCountDesc),
      other: [...merged.other].sort(byCountDesc),
    };
  }, [query.data]);
  const columns = useMemo(() => makeInputColumns(setSetup), [setSetup]);

  const setupOpen = search.setup !== undefined;
  const setupId = search.setup ? search.setup : undefined;

  return (
    <div className={styles.page}>
      <div className={styles.header}>
        <h1>Inputs</h1>
        <Button variant="primary" leadingIcon="plug" onClick={() => setSetup("")}>
          How to receive alerts
        </Button>
      </div>

      {query.isError ? (
        // Don't let a failed activity fetch masquerade as "every input is idle":
        // the catalogue below still renders, but flag that the counts/last-seen
        // are unavailable so the operator doesn't misread it while debugging why
        // alerts stopped flowing.
        <Card padded>
          <p className={styles.hint}>
            Couldn&apos;t load ingestion activity — the “last received” and alert counts below may be
            missing or stale.
          </p>
          <Button size="sm" variant="secondary" onClick={() => void query.refetch()}>
            Retry
          </Button>
        </Card>
      ) : null}

      <DataTable<InputRow>
        data={inputs}
        columns={columns}
        rowKey={(r) => r.id}
        loading={query.isPending}
        emptyState={
          <EmptyState icon="plug" title="No inputs" description="No supported inputs found." />
        }
      />

      {other.length > 0 ? (
        <Card padded>
          <h2 className={styles.cardTitle}>Other sources</h2>
          <p className={styles.hint}>
            Alert sources seen in records that don&apos;t match a known input — custom REST posters,
            or receivers without a catalogue entry.
          </p>
          <DataTable<InputRow> data={other} columns={columns} rowKey={(r) => r.id} />
        </Card>
      ) : null}

      <InjectAlertsDialog
        open={setupOpen}
        onOpenChange={(open) => {
          if (!open) setSetup(undefined);
        }}
        {...(setupId !== undefined ? { initialSourceId: setupId } : {})}
      />
    </div>
  );
}
