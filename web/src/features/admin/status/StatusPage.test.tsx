import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { LiveAnnouncerProvider } from "@/shared/a11y/LiveAnnouncer";
import { StatusPage } from "./StatusPage";

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("StatusPage", () => {
  it("renders cluster members + plugins from the API", async () => {
    mswServer.use(
      http.get("/api/v1/cluster/status", () =>
        HttpResponse.json({
          cluster: {
            members: [
              { name: "snooze1", status: "ok" },
              { name: "snooze2", status: "degraded" },
            ],
            leader: "snooze1",
          },
          plugins: [
            { name: "rule", loaded: true },
            { name: "snooze", loaded: true },
          ],
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <StatusPage />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByText("snooze1")).toBeInTheDocument());
    expect(screen.getByText("snooze2")).toBeInTheDocument();
    // All plugins loaded → the 40-rows-of-"yes" table folds behind a summary
    // line instead of dumping every row on screen.
    expect(screen.getByText("2/2 plugins loaded")).toBeInTheDocument();
    expect(screen.queryByText("rule")).not.toBeInTheDocument();
    await userEvent.click(screen.getByText("2/2 plugins loaded"));
    expect(screen.getByText("rule")).toBeInTheDocument();
    // One-glance verdict banner: snooze2 is degraded → one issue detected.
    expect(screen.getByText(/1 issue detected/i)).toBeInTheDocument();
    // Freshness caption.
    expect(screen.getByText(/updated/i)).toBeInTheDocument();
  });

  it("keeps the plugin table expanded and loud when a plugin fails to load", async () => {
    mswServer.use(
      http.get("/api/v1/cluster/status", () =>
        HttpResponse.json({
          cluster: { members: [{ name: "snooze1", status: "ok" }], leader: "snooze1" },
          plugins: [
            { name: "rule", loaded: true },
            { name: "broken-notifier", loaded: false },
          ],
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <StatusPage />
      </Wrapper>,
    );
    // No summary/disclosure to click through — the failure is visible immediately.
    await waitFor(() => expect(screen.getByText("broken-notifier")).toBeInTheDocument());
    expect(screen.getByText("rule")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent(/1 of 2 plugins failed to load/i);
  });

  it("shows an all-clear verdict when everything is healthy", async () => {
    mswServer.use(
      http.get("/api/v1/cluster/status", () =>
        HttpResponse.json({
          cluster: { members: [{ name: "snooze1", status: "ok" }], leader: "snooze1" },
          plugins: [{ name: "rule", loaded: true }],
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <StatusPage />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByText(/all systems operational/i)).toBeInTheDocument());
  });

  it("shows an empty state when the API errors", async () => {
    mswServer.use(
      http.get("/api/v1/cluster/status", () => new HttpResponse(null, { status: 404 })),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <StatusPage />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByText(/not available/i)).toBeInTheDocument());
  });
});

// ── Screen-reader parity for the 15s poll ─────────────────────────────────

describe("StatusPage — health-flip announcements", () => {
  it("interrupts when the cluster breaks and waits its turn when it recovers", async () => {
    let healthy = true;
    mswServer.use(
      http.get("/api/v1/cluster/status", () =>
        HttpResponse.json({
          cluster: {
            members: [{ name: "snooze1", status: healthy ? "ok" : "down" }],
            leader: "snooze1",
          },
          plugins: [{ name: "rule", loaded: true }],
        }),
      ),
    );
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <LiveAnnouncerProvider>
          <StatusPage />
        </LiveAnnouncerProvider>
      </QueryClientProvider>,
    );
    await waitFor(() => expect(screen.getByText(/all systems operational/i)).toBeInTheDocument());
    // Landing on a healthy page is not a flip.
    expect(screen.getByTestId("live-assertive").textContent).toBe("");

    healthy = false;
    await act(async () => {
      await client.refetchQueries();
    });
    await waitFor(() =>
      expect(screen.getByTestId("live-assertive")).toHaveTextContent(
        "Cluster status: 1 issue detected.",
      ),
    );

    healthy = true;
    await act(async () => {
      await client.refetchQueries();
    });
    await waitFor(() =>
      expect(screen.getByTestId("live-polite")).toHaveTextContent(
        "Cluster status: All systems operational.",
      ),
    );
  });
});
