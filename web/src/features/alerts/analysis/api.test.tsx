import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { ApiError } from "@/lib/api/client";
import { Records } from "../api";
import {
  ANALYSIS_QUERY_KEY,
  analysisFieldErrors,
  analysisPath,
  analysisQueryKey,
  useAnalysis,
  useClearAnalysis,
  useSetAnalysis,
  type AgenticEnvelope,
  type AgenticRequest,
} from "./api";

const ENVELOPE: AgenticEnvelope = {
  uid: "a1",
  agentic: {
    root_cause: { summary: "Disk filled with journals", confidence: "high" },
    remediation_plan: { steps: [{ action: "Vacuum the journal", risk: "low" }] },
    analysis: { at: "2026-09-21T10:00:00Z", by: "alice", source: "alert-rca" },
  },
};

const REQUEST: AgenticRequest = {
  root_cause: { summary: "Disk filled with journals", confidence: "high" },
  remediation_plan: { steps: [{ action: "Vacuum the journal", risk: "low" }] },
  source: "snooze-web",
};

function makeClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

function wrap(client: QueryClient) {
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

/** Every key a mutation invalidated, flattened for readable assertions. */
function invalidatedKeys(spy: { mock: { calls: unknown[][] } }): unknown[][] {
  return spy.mock.calls.map((call) => {
    const arg = call[0] as { queryKey?: unknown[] } | undefined;
    return arg?.queryKey ?? [];
  });
}

describe("analysisPath", () => {
  it("addresses the analysis through its parent record", () => {
    expect(analysisPath("a1")).toBe("/record/a1/agentic");
  });

  it("escapes a uid so it cannot open a second path segment", () => {
    expect(analysisPath("a/../role")).toBe("/record/a%2F..%2Frole/agentic");
  });
});

describe("useAnalysis", () => {
  it("returns the stored analysis", async () => {
    mswServer.use(http.get("/api/v1/record/a1/agentic", () => HttpResponse.json(ENVELOPE)));
    const { result } = renderHook(() => useAnalysis("a1"), { wrapper: wrap(makeClient()) });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual(ENVELOPE);
  });

  it("maps a 404 to null data — 'no analysis' is the normal state, not a failure", async () => {
    // Almost every alert is unanalysed. Surfacing that as an error would put a
    // red box in the inspector of every alert anyone opens.
    mswServer.use(
      http.get("/api/v1/record/a1/agentic", () =>
        HttpResponse.json(
          { error: { code: "not_found", message: "record carries no analysis" } },
          { status: 404 },
        ),
      ),
    );
    const { result } = renderHook(() => useAnalysis("a1"), { wrapper: wrap(makeClient()) });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toBeNull();
    expect(result.current.isError).toBe(false);
  });

  it("still reports any other failure", async () => {
    mswServer.use(
      http.get("/api/v1/record/a1/agentic", () =>
        HttpResponse.json({ error: { code: "forbidden", message: "nope" } }, { status: 403 }),
      ),
    );
    const { result } = renderHook(() => useAnalysis("a1"), { wrapper: wrap(makeClient()) });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error?.status).toBe(403);
  });

  it("never fires without a uid, and not at all when disabled", async () => {
    let calls = 0;
    mswServer.use(
      http.get("/api/v1/record/*/agentic", () => {
        calls += 1;
        return HttpResponse.json(ENVELOPE);
      }),
    );
    const undef = renderHook(() => useAnalysis(undefined), { wrapper: wrap(makeClient()) });
    const empty = renderHook(() => useAnalysis(""), { wrapper: wrap(makeClient()) });
    const off = renderHook(() => useAnalysis("a1", { enabled: false }), {
      wrapper: wrap(makeClient()),
    });
    await Promise.resolve();
    expect(calls).toBe(0);
    for (const hook of [undef, empty, off]) {
      expect(hook.result.current.fetchStatus).toBe("idle");
    }
  });

  it("keys the cache by uid so two inspectors cannot share an analysis", () => {
    expect(analysisQueryKey("a1")).toEqual([ANALYSIS_QUERY_KEY, "a1"]);
    expect(analysisQueryKey("a2")).not.toEqual(analysisQueryKey("a1"));
  });
});

describe("useSetAnalysis", () => {
  it("PUTs the request body and returns the stamped envelope", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.put("/api/v1/record/a1/agentic", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json(ENVELOPE);
      }),
    );
    const { result } = renderHook(() => useSetAnalysis(), { wrapper: wrap(makeClient()) });
    result.current.mutate({ uid: "a1", body: REQUEST });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(bodies).toEqual([REQUEST]);
    expect(result.current.data).toEqual(ENVELOPE);
  });

  it("invalidates the analysis AND the record lists", async () => {
    // The severity cell's confidence dot and the inspector header line read
    // off the record document, not off this route: invalidating only our own
    // key would leave the table showing the previous confidence.
    mswServer.use(http.put("/api/v1/record/a1/agentic", () => HttpResponse.json(ENVELOPE)));
    const client = makeClient();
    const spy = vi.spyOn(client, "invalidateQueries");
    const { result } = renderHook(() => useSetAnalysis(), { wrapper: wrap(client) });
    result.current.mutate({ uid: "a1", body: REQUEST });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(invalidatedKeys(spy)).toEqual([[ANALYSIS_QUERY_KEY, "a1"], [...Records.queryKey.all]]);
  });

  it("does not invalidate anything when the write failed", async () => {
    mswServer.use(
      http.put("/api/v1/record/a1/agentic", () =>
        HttpResponse.json({ error: { code: "forbidden", message: "nope" } }, { status: 403 }),
      ),
    );
    const client = makeClient();
    const spy = vi.spyOn(client, "invalidateQueries");
    const { result } = renderHook(() => useSetAnalysis(), { wrapper: wrap(client) });
    result.current.mutate({ uid: "a1", body: REQUEST });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(spy).not.toHaveBeenCalled();
  });
});

describe("useClearAnalysis", () => {
  it("DELETEs and invalidates both keys", async () => {
    let seen = "";
    mswServer.use(
      http.delete("/api/v1/record/a1/agentic", ({ request }) => {
        seen = request.method;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const client = makeClient();
    const spy = vi.spyOn(client, "invalidateQueries");
    const { result } = renderHook(() => useClearAnalysis(), { wrapper: wrap(client) });
    result.current.mutate("a1");
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(seen).toBe("DELETE");
    expect(invalidatedKeys(spy)).toEqual([[ANALYSIS_QUERY_KEY, "a1"], [...Records.queryKey.all]]);
  });
});

describe("analysisFieldErrors", () => {
  it("reads the server's per-field messages off a real 422", async () => {
    // End-to-end through the api client, because the details map only reaches
    // a caller if the client preserves it while parsing the error envelope.
    mswServer.use(
      http.put("/api/v1/record/a1/agentic", () =>
        HttpResponse.json(
          {
            error: {
              code: "validation_error",
              message: "agentic payload failed schema validation",
              details: {
                "root_cause.confidence": "must be one of high|medium|low",
                "remediation_plan.steps[0].risk": "must be one of low|medium|high",
              },
            },
          },
          { status: 422 },
        ),
      ),
    );
    const { result } = renderHook(() => useSetAnalysis(), { wrapper: wrap(makeClient()) });
    result.current.mutate({ uid: "a1", body: REQUEST });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(analysisFieldErrors(result.current.error)).toEqual({
      "root_cause.confidence": "must be one of high|medium|low",
      "remediation_plan.steps[0].risk": "must be one of low|medium|high",
    });
  });

  it("keeps only string values — a details map is free-form", () => {
    const err = new ApiError(422, "validation_error", "bad", undefined, {
      "root_cause.summary": "is required",
      "root_cause.evidence": ["not", "a", "message"],
      "remediation_plan.steps": 3,
    });
    expect(analysisFieldErrors(err)).toEqual({ "root_cause.summary": "is required" });
  });

  it("yields {} for anything that is not a field failure", () => {
    // A caller should be able to ask without proving the error's shape first.
    expect(analysisFieldErrors(new ApiError(403, "forbidden", "nope"))).toEqual({});
    expect(analysisFieldErrors(new Error("network down"))).toEqual({});
    expect(analysisFieldErrors("boom")).toEqual({});
    expect(analysisFieldErrors(null)).toEqual({});
    expect(analysisFieldErrors(undefined)).toEqual({});
  });
});
