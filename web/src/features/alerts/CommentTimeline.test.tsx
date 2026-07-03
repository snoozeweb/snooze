import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi, afterEach } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { authStore } from "@/lib/auth/store";
import { toastStore } from "@/shared/ui/toast/useToast";
import { CommentTimeline } from "./CommentTimeline";

function wrap() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

function loginWithPerms(perms: string[]) {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({ sub: "tester", exp: Math.floor(Date.now() / 1000) + 3600, permissions: perms }),
  );
  authStore.getState().login(`${header}.${body}.sig`);
}

describe("CommentTimeline", () => {
  afterEach(() => {
    vi.useRealTimers();
    authStore.getState().logout();
    toastStore.clear();
  });

  it("posting an ack from the composer resyncs the alert list (invalidates record queries)", async () => {
    // The composer can drive a real state transition (ack/esc). If it only
    // invalidates comment queries, the alert table/badge stay stale until an
    // unrelated refetch. It must invalidate record queries too.
    loginWithPerms(["can_comment"]);
    const bodies: Array<{ type: string }> = [];
    mswServer.use(
      http.get("/api/v1/comment", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 5, offset: 0, total: 0 } }),
      ),
      http.post("/api/v1/comment", async ({ request }) => {
        bodies.push((await request.json()) as { type: string });
        return HttpResponse.json({ ok: true });
      }),
    );
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    const spy = vi.spyOn(client, "invalidateQueries");
    const user = userEvent.setup();
    render(
      <QueryClientProvider client={client}>
        <CommentTimeline recordUid="r1" />
      </QueryClientProvider>,
    );

    await screen.findByLabelText(/new comment/i);
    // Switch the composer to "acknowledged", write a note, and submit. The
    // submit button is action-labelled, so it reads "Acknowledge" here.
    await user.click(screen.getByRole("button", { name: /acknowledged/i }));
    await user.type(screen.getByLabelText(/new comment/i), "on it");
    await act(async () => {
      await user.click(screen.getByRole("button", { name: /^acknowledge$/i }));
    });

    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toMatchObject({ type: "ack" });
    const invalidatedKeys = spy.mock.calls.map((c) => JSON.stringify(c[0]?.queryKey));
    expect(invalidatedKeys).toContain(JSON.stringify(["record"]));
  });

  it("labels the submit button by the selected action and offers undo on a state change", async () => {
    // A single generic "Post" button that silently changes alert state is a
    // doomed-affordance smell. The button must name what it does, and a
    // state-changing post must be undoable like the rest of the app.
    loginWithPerms(["can_comment"]);
    const posts: Array<{ type: string }> = [];
    mswServer.use(
      http.get("/api/v1/comment", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 5, offset: 0, total: 0 } }),
      ),
      http.post("/api/v1/comment", async ({ request }) => {
        posts.push((await request.json()) as { type: string });
        return HttpResponse.json({ ok: true });
      }),
    );
    const user = userEvent.setup();
    render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <CommentTimeline recordUid="r1" />
      </QueryClientProvider>,
    );
    await screen.findByLabelText(/new comment/i);
    // Default type is comment → the submit reads "Comment", not "Post".
    expect(screen.getByRole("button", { name: /^comment$/i })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^post$/i })).toBeNull();
    // Switch to acknowledge → the submit relabels.
    await user.click(screen.getByRole("button", { name: /acknowledged/i }));
    expect(screen.getByRole("button", { name: /^acknowledge$/i })).toBeInTheDocument();
    // Posting the ack raises an undo toast.
    await user.type(screen.getByLabelText(/new comment/i), "on it");
    await act(async () => {
      await user.click(screen.getByRole("button", { name: /^acknowledge$/i }));
    });
    await waitFor(() => expect(posts.some((p) => p.type === "ack")).toBe(true));
    await waitFor(() => expect(toastStore.getSnapshot().some((t) => t.action)).toBe(true));
  });

  it("confirms before deleting a comment", async () => {
    loginWithPerms(["can_comment", "rw_record"]);
    let deleted = 0;
    mswServer.use(
      http.get("/api/v1/comment", () =>
        HttpResponse.json({
          data: [
            {
              uid: "c1",
              record_uid: "r1",
              type: "comment",
              message: "note here",
              user: "tester",
              date_epoch: 100,
            },
          ],
          meta: { count: 1, limit: 5, offset: 0, total: 1 },
        }),
      ),
      http.delete("/api/v1/comment/c1", () => {
        deleted += 1;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <CommentTimeline recordUid="r1" />
      </QueryClientProvider>,
    );
    await screen.findByText("note here");
    await user.click(screen.getByRole("button", { name: /delete comment/i }));
    const dialog = await screen.findByRole("dialog");
    expect(deleted).toBe(0);
    await user.click(within(dialog).getByRole("button", { name: /delete/i }));
    await waitFor(() => expect(deleted).toBe(1));
  });

  it("hides composer ack/esc chips the backend would reject for the record's state", async () => {
    // A closed alert can only be re-opened; ack/esc from here would 403. The
    // composer must gate its transition chips on the record's state, like the
    // rest of the page — comment stays available.
    loginWithPerms(["can_comment"]);
    mswServer.use(
      http.get("/api/v1/comment", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 5, offset: 0, total: 0 } }),
      ),
    );
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <CommentTimeline recordUid="r1" state="close" />
      </QueryClientProvider>,
    );
    await screen.findByLabelText(/new comment/i);
    expect(screen.getByRole("button", { name: /commented/i })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /acknowledged/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /re-escalated/i })).toBeNull();
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

  it("auto_comment_shows_system_attribution — auto:true attributed as 'System (auto)'", async () => {
    mswServer.use(
      http.get("/api/v1/comment", () =>
        HttpResponse.json({
          data: [
            {
              uid: "c-auto",
              record_uid: "r1",
              type: "comment",
              message: "Ack expired, reverted to open",
              date_epoch: 1000,
              user: null,
              auto: true,
            },
          ],
          meta: { count: 1, limit: 5, offset: 0, total: 1 },
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <CommentTimeline recordUid="r1" />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByText(/System \(auto\)/)).toBeInTheDocument());
  });

  it("auto_comment_hides_edit_delete — no Edit/Delete buttons on auto comment", async () => {
    mswServer.use(
      http.get("/api/v1/comment", () =>
        HttpResponse.json({
          data: [
            {
              uid: "c-auto",
              record_uid: "r1",
              type: "comment",
              message: "Auto-generated system note",
              date_epoch: 1000,
              user: "system",
              auto: true,
            },
          ],
          meta: { count: 1, limit: 5, offset: 0, total: 1 },
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <CommentTimeline recordUid="r1" />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByText(/System \(auto\)/)).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: /edit comment/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /delete comment/i })).toBeNull();
  });

  it("manual_comment_shows_user_attribution — regular comment shows user name", async () => {
    mswServer.use(
      http.get("/api/v1/comment", () =>
        HttpResponse.json({
          data: [
            {
              uid: "c-manual",
              record_uid: "r1",
              type: "comment",
              message: "Investigating now",
              date_epoch: 1000,
              user: "alice",
            },
          ],
          meta: { count: 1, limit: 5, offset: 0, total: 1 },
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <CommentTimeline recordUid="r1" />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByText(/alice/)).toBeInTheDocument());
    // Edit/Delete shown when user matches (useAuth returns empty sub by default but
    // canModerate=false too — so no edit in test env; just verify user is shown)
    expect(screen.queryByText(/System \(auto\)/)).toBeNull();
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
