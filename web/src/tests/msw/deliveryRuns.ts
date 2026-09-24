// Test helper: serve GET /api/v1/notificationlog/runs from a list of log rows,
// each row a run of one. Good enough for tests that are about the rows, not
// the fold (the fold itself is the server's and tested there); a test about
// runs stubs the route with real runs instead.
import { http, HttpResponse } from "msw";
import type {
  DeliveryEntry,
  DeliveryRunsResponse,
} from "@/features/notifications/deliveries/types";

export function runsOfOne(rows: readonly DeliveryEntry[]): DeliveryRunsResponse {
  const epochs = rows.map((r) => r.date_epoch ?? 0);
  return {
    data: rows.map((row, i) => ({
      key: row.uid ?? `run-${i}`,
      dispatches: 1,
      sends: 1,
      first_epoch: row.date_epoch ?? 0,
      last_epoch: row.date_epoch ?? 0,
      interval_s: 0,
      latest: [row],
      truncated: false,
    })),
    meta: {
      total: rows.length,
      sends: rows.length,
      dispatches: rows.length,
      first_epoch: epochs.length ? Math.min(...epochs) : 0,
      last_epoch: epochs.length ? Math.max(...epochs) : 0,
      interval_s: 0,
      truncated: false,
    },
  };
}

/** A runs handler over `rowsFor(alertUid)`, paged by limit/offset. */
export function runsHandler(rowsFor: (alertUid: string) => readonly DeliveryEntry[]) {
  return http.get("/api/v1/notificationlog/runs", ({ request }) => {
    const url = new URL(request.url);
    const all = runsOfOne(rowsFor(url.searchParams.get("alert_uid") ?? ""));
    const limit = Number(url.searchParams.get("limit") ?? "10");
    const offset = Number(url.searchParams.get("offset") ?? "0");
    return HttpResponse.json({ ...all, data: all.data.slice(offset, offset + limit) });
  });
}
