import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { AuditTimeline } from "./AuditTimeline";

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

beforeEach(() => {
  // Default handler: the real list call hits /api/v1/audit with a q
  // parameter that filters on object_type+object_id. Individual tests
  // override this with the rows they want returned.
  mswServer.use(
    http.get("/api/v1/audit", () =>
      HttpResponse.json({ data: [], meta: { count: 0, limit: 200, offset: 0, total: 0 } }),
    ),
  );
});

describe("AuditTimeline", () => {
  it("shows the create-mode hint when objectId is undefined", () => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <AuditTimeline objectType="rule" objectId={undefined} />
      </Wrapper>,
    );
    expect(
      screen.getByText(/Audit log appears here once the rule is saved/i),
    ).toBeInTheDocument();
  });

  it("renders the empty state when the server returns no rows", async () => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <AuditTimeline objectType="rule" objectId="r1" />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByText("No changes recorded yet.")).toBeInTheDocument());
  });

  it("renders one row per audit entry with the action label and user", async () => {
    mswServer.use(
      http.get("/api/v1/audit", () =>
        HttpResponse.json({
          data: [
            {
              uid: "a1",
              object_type: "rule",
              object_id: "r1",
              action: "create",
              username: "alice",
              date_epoch: 1747300000,
            },
            {
              uid: "a2",
              object_type: "rule",
              object_id: "r1",
              action: "patch",
              username: "bob",
              summary: "enabled, tree_order",
              date_epoch: 1747300600,
            },
          ],
          meta: { count: 2, limit: 200, offset: 0, total: 2 },
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <AuditTimeline objectType="rule" objectId="r1" />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByText("created")).toBeInTheDocument());
    expect(screen.getByText("edited")).toBeInTheDocument();
    expect(screen.getByText("enabled, tree_order")).toBeInTheDocument();
    // Username + relative-time meta line includes "alice" and "bob".
    expect(screen.getByText(/alice/)).toBeInTheDocument();
    expect(screen.getByText(/bob/)).toBeInTheDocument();
  });

  it("falls back to 'system' when username is missing", async () => {
    mswServer.use(
      http.get("/api/v1/audit", () =>
        HttpResponse.json({
          data: [
            {
              uid: "a1",
              object_type: "rule",
              object_id: "r1",
              action: "delete",
              date_epoch: 1747300000,
            },
          ],
          meta: { count: 1, limit: 200, offset: 0, total: 1 },
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <AuditTimeline objectType="rule" objectId="r1" />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByText("deleted")).toBeInTheDocument());
    expect(screen.getByText(/system/)).toBeInTheDocument();
  });
});
