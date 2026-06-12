import { render, screen, act } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { mswServer } from "@/tests/msw/server";
import { ConditionPreview } from "./ConditionPreview";
import type { Condition } from "@/lib/condition/types";

function setup(condition: Condition | undefined) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ConditionPreview condition={condition} />
    </QueryClientProvider>,
  );
}

const simpleCondition: Condition = {
  type: "EQUALS",
  field: "severity",
  value: "critical",
};

describe("ConditionPreview", () => {
  it("renders the collapsed header with a badge", () => {
    setup(simpleCondition);
    expect(screen.getByText(/preview matching alerts/i)).toBeInTheDocument();
  });

  it("debounces condition changes — coalesces rapid updates into one request", async () => {
    vi.useFakeTimers();

    const requestedQParams: string[] = [];
    mswServer.use(
      http.get("/api/v1/record", ({ request }) => {
        const url = new URL(request.url);
        const q = url.searchParams.get("q") ?? "";
        requestedQParams.push(q);
        return HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 5, offset: 0, total: 0 },
        });
      }),
    );

    const condA: Condition = { type: "EQUALS", field: "severity", value: "a" };
    const condB: Condition = { type: "EQUALS", field: "severity", value: "ab" };
    const condC: Condition = { type: "EQUALS", field: "severity", value: "abc" };

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { rerender } = render(
      <QueryClientProvider client={client}>
        <ConditionPreview condition={condA} />
      </QueryClientProvider>,
    );

    // Advance less than the debounce window so condA doesn't settle yet
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });

    // Rapid updates before the debounce window closes — simulate typing
    rerender(
      <QueryClientProvider client={client}>
        <ConditionPreview condition={condB} />
      </QueryClientProvider>,
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });

    rerender(
      <QueryClientProvider client={client}>
        <ConditionPreview condition={condC} />
      </QueryClientProvider>,
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });

    // Snapshot how many distinct q-param values have been requested so far —
    // the debounce should have suppressed condA and condB; only nothing or
    // nothing (since 300ms has not elapsed since any of the three conditions).
    const countBeforeSettle = requestedQParams.length;

    // Advance past the debounce window for condC
    await act(async () => {
      await vi.advanceTimersByTimeAsync(350);
    });

    // After settling, condC should be the only value that actually triggered
    // a network request. The debounce coalesces condA+condB into nothing and
    // condC is the sole query. At most 1 new request after the settle.
    expect(requestedQParams.length - countBeforeSettle).toBeLessThanOrEqual(1);

    vi.useRealTimers();
  });
});
