// Test helper: serve GET /api/v1/comment/runs by asking whatever GET
// /api/v1/comment handler the test installed, each comment a run of one. Lets
// the timeline's existing tests keep stubbing the plain list; a test about
// folding stubs /comment/runs with real runs instead.
import { http, HttpResponse } from "msw";
import type { Comment, CommentRunsResponse } from "@/features/alerts/comments";

export const commentRunsViaList = http.get("/api/v1/comment/runs", async ({ request }) => {
  const url = new URL(request.url);
  const list = new URL("/api/v1/comment", url);
  for (const k of ["limit", "offset"]) {
    const v = url.searchParams.get(k);
    if (v !== null) list.searchParams.set(k, v);
  }
  const res = await fetch(list);
  if (!res.ok) return new HttpResponse(null, { status: res.status });
  const body = (await res.json()) as { data: Comment[]; meta: { total: number } };
  const out: CommentRunsResponse = {
    data: body.data.map((c, i) => ({
      key: c.uid ?? `run-${i}`,
      count: 1,
      first_epoch: c.date_epoch ?? 0,
      last_epoch: c.date_epoch ?? 0,
      interval_s: 0,
      latest: c,
      truncated: false,
    })),
    meta: { total: body.meta.total, comments: body.meta.total, truncated: false },
  };
  return HttpResponse.json(out);
});
