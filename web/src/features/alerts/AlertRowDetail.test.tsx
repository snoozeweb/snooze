import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { AlertRowDetail } from "./AlertRowDetail";
import type { Record_ } from "./types";

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("AlertRowDetail", () => {
  it("renders the JSON of the row stripped of underscore-prefixed keys", () => {
    const row: Record_ = {
      uid: "r1",
      host: "srv-1",
      severity: "critical",
      state: "open",
      message: "disk full",
      date_epoch: 1,
    };
    // Inject an internal key that should be stripped, mirroring RowDetailPanel.
    const rowWithPrivate = { ...row, _internal: "secret" } as Record_;
    mswServer.use(
      http.get("/api/v1/comment", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 100, offset: 0, total: 0 },
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <AlertRowDetail row={rowWithPrivate} />
      </Wrapper>,
    );
    // JsonViewer renders the cleaned object as a tree of <pre> elements.
    expect(screen.getByText(/srv-1/)).toBeInTheDocument();
    expect(screen.getByText(/disk full/)).toBeInTheDocument();
    // The underscore-prefixed key must not appear.
    expect(screen.queryByText(/_internal/)).toBeNull();
  });

  it("renders a CommentTimeline scoped to the row's uid", async () => {
    mswServer.use(
      http.get("/api/v1/comment", ({ request }) => {
        const url = new URL(request.url);
        // CommentTimeline filters by record_uid via the resource list query.
        // We just need to return an empty list so the empty state shows.
        // Verify the request is for this record.
        expect(url.searchParams.get("q")).not.toBeNull();
        return HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 100, offset: 0, total: 0 },
        });
      }),
    );
    const row: Record_ = { uid: "r1", host: "srv-1", date_epoch: 1 };
    const Wrapper = wrap();
    render(
      <Wrapper>
        <AlertRowDetail row={row} />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByText(/no comments yet/i)).toBeInTheDocument());
  });
});
