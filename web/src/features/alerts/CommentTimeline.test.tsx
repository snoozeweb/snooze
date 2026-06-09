import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi, afterEach } from "vitest";
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
  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders the comment date in the alert-table format (trimDate), not relative", async () => {
    // Fix the clock so trimDate and formatRelativeTime both see the same "now".
    // Use Jan 15 2026 12:00:00 UTC as "now"; fix epoch = Jan 10 2026 09:30:00 UTC
    // → same year, different day → trimDate yields "Jan 10th 09:30" (ordinal form).
    const nowMs = new Date("2026-01-15T12:00:00Z").getTime();
    const commentEpochSec = Math.floor(new Date("2026-01-10T09:30:00Z").getTime() / 1000);
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.setSystemTime(nowMs);

    mswServer.use(
      http.get("/api/v1/comment", () =>
        HttpResponse.json({
          data: [
            {
              uid: "c-date",
              record_uid: "r2",
              type: "comment",
              message: "date format check",
              date_epoch: commentEpochSec,
              user: "alice",
            },
          ],
          meta: { count: 1, limit: 100, offset: 0, total: 1 },
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <CommentTimeline recordUid="r2" />
      </Wrapper>,
    );

    // trimDate for a same-year non-today date renders "MMM Dth HH:mm"
    await waitFor(() =>
      expect(screen.getByText(/\b\d{1,2}(st|nd|rd|th)\b|Today/)).toBeInTheDocument(),
    );
    // Must NOT render a relative token like "5d", "3h", "42m", "7s"
    expect(screen.queryByText(/^\d+[smhd]$/)).not.toBeInTheDocument();
  });

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

  it("offers first/last page jumps that move to the page boundaries", async () => {
    const user = userEvent.setup();
    // total 12, default pageSize 5 → 3 pages, so the pagination bar shows.
    mswServer.use(
      http.get("/api/v1/comment", () =>
        HttpResponse.json({
          data: [
            {
              uid: "c1",
              record_uid: "r1",
              type: "comment",
              message: "hi",
              date_epoch: 100,
              user: "alice",
            },
          ],
          meta: { count: 1, limit: 5, offset: 0, total: 12 },
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <CommentTimeline recordUid="r1" />
      </Wrapper>,
    );

    await waitFor(() => expect(screen.getByText(/Page 1 \/ 3/)).toBeInTheDocument());
    // On page 1 the backward jumps are disabled, the forward ones enabled.
    expect(screen.getByRole("button", { name: /first page/i })).toBeDisabled();
    expect(screen.getByRole("button", { name: /last page/i })).toBeEnabled();

    await user.click(screen.getByRole("button", { name: /last page/i }));
    await waitFor(() => expect(screen.getByText(/Page 3 \/ 3/)).toBeInTheDocument());
    expect(screen.getByRole("button", { name: /last page/i })).toBeDisabled();
    expect(screen.getByRole("button", { name: /first page/i })).toBeEnabled();

    await user.click(screen.getByRole("button", { name: /first page/i }));
    await waitFor(() => expect(screen.getByText(/Page 1 \/ 3/)).toBeInTheDocument());
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
