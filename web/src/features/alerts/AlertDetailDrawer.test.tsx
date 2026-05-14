import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { AlertDetailDrawer } from "./AlertDetailDrawer";

function wrap() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("AlertDetailDrawer", () => {
  it("does not render when uid is undefined", () => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <AlertDetailDrawer uid={undefined} onClose={() => undefined} />
      </Wrapper>,
    );
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("loads the record and shows host + message + tags + timeline empty state", async () => {
    mswServer.use(
      http.get("/api/v1/record/r1", () =>
        HttpResponse.json({
          uid: "r1",
          host: "srv-1",
          severity: "critical",
          state: "open",
          message: "disk full",
          tags: ["pd", "noisy"],
          date_epoch: Math.floor(Date.now() / 1000) - 60,
        }),
      ),
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
        <AlertDetailDrawer uid="r1" onClose={() => undefined} />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByRole("dialog", { name: /srv-1/i })).toBeInTheDocument());
    expect(screen.getByText(/disk full/)).toBeInTheDocument();
    expect(screen.getByText("pd")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText(/no comments yet/i)).toBeInTheDocument());
  });

  it("renders MailPane when smtp fields are present", async () => {
    mswServer.use(
      http.get("/api/v1/record/r2", () =>
        HttpResponse.json({
          uid: "r2",
          host: "smtp.example.com",
          smtp_from: "alert@example.com",
          smtp_subject: "Disk usage critical",
          smtp_body: "Volume /var is at 95%.",
          date_epoch: 1,
        }),
      ),
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
        <AlertDetailDrawer uid="r2" onClose={() => undefined} />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByText("Disk usage critical")).toBeInTheDocument());
    expect(screen.getByText(/alert@example\.com/)).toBeInTheDocument();
  });

  it("invokes onClose when Escape closes the drawer", async () => {
    mswServer.use(
      http.get("/api/v1/record/r1", () =>
        HttpResponse.json({ uid: "r1", host: "srv-1", date_epoch: 1 }),
      ),
      http.get("/api/v1/comment", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 100, offset: 0, total: 0 },
        }),
      ),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    render(
      <Wrapper>
        <AlertDetailDrawer uid="r1" onClose={onClose} />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());
    const dialog = screen.getByRole("dialog");
    dialog.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });
});
