import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { CommentTimeline } from "./CommentTimeline";

function wrap() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("CommentTimeline", () => {
  it("renders 'No comments yet' when empty", async () => {
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
        <CommentTimeline recordUid="r1" />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByText(/no comments yet/i)).toBeInTheDocument());
  });

  it("renders one row per comment", async () => {
    mswServer.use(
      http.get("/api/v1/comment", () =>
        HttpResponse.json({
          data: [
            {
              uid: "c1",
              record_uid: "r1",
              type: "ack",
              message: "got it",
              date_epoch: 100,
              user: "alice",
            },
            { uid: "c2", record_uid: "r1", type: "close", date_epoch: 200, user: "bob" },
          ],
          meta: { count: 2, limit: 100, offset: 0, total: 2 },
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <CommentTimeline recordUid="r1" />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByText(/acknowledged/i)).toBeInTheDocument());
    expect(screen.getByText(/closed/i)).toBeInTheDocument();
    expect(screen.getByText(/got it/)).toBeInTheDocument();
  });
});
