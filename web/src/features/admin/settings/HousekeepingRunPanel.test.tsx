import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { beforeAll, describe, expect, it } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { HousekeepingRunPanel } from "./HousekeepingRunPanel";

beforeAll(() => {
  if (typeof window !== "undefined" && !window.ResizeObserver) {
    window.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider delay={0}>
        <ToastProvider>
          <HousekeepingRunPanel />
          <Toaster />
        </ToastProvider>
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("HousekeepingRunPanel", () => {
  it("shows the registered-job count from the status endpoint", async () => {
    mswServer.use(
      http.get("/api/v1/housekeeping/status", () =>
        HttpResponse.json({ status: "ok", registered_jobs: 5 }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText(/5 jobs registered/i)).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Run now" })).toBeEnabled();
  });

  it("renders a not-configured state and disables Run when status is 503", async () => {
    mswServer.use(
      http.get("/api/v1/housekeeping/status", () =>
        HttpResponse.json(
          { error: { code: "unavailable", message: "housekeeper not configured" } },
          { status: 503 },
        ),
      ),
    );
    setup();
    await waitFor(() =>
      expect(screen.getByText(/Housekeeper is not configured/i)).toBeInTheDocument(),
    );
    expect(screen.getByRole("button", { name: "Run now" })).toBeDisabled();
  });

  it("confirms, POSTs the run, and shows per-job results with a success toast", async () => {
    let posted = 0;
    mswServer.use(
      http.get("/api/v1/housekeeping/status", () =>
        HttpResponse.json({ status: "ok", registered_jobs: 2 }),
      ),
      http.post("/api/v1/housekeeping/run", () => {
        posted += 1;
        return HttpResponse.json({
          status: "ok",
          jobs: [
            { name: "cleanup_snooze", duration_ms: 12 },
            { name: "cleanup_audit", duration_ms: 5 },
          ],
          errors: 0,
        });
      }),
    );
    setup();
    const user = userEvent.setup();
    await waitFor(() => expect(screen.getByText(/2 jobs registered/i)).toBeInTheDocument());

    // Trigger opens the confirm dialog; nothing is POSTed yet.
    await user.click(screen.getByRole("button", { name: "Run now" }));
    const confirm = await screen.findByRole("button", { name: "Run housekeeping" });
    expect(posted).toBe(0);

    await user.click(confirm);
    await waitFor(() => expect(posted).toBe(1));

    // Per-job results render.
    expect(await screen.findByText("cleanup_snooze")).toBeInTheDocument();
    expect(screen.getByText("cleanup_audit")).toBeInTheDocument();
    expect(screen.getByText("12 ms")).toBeInTheDocument();
    // Success toast summarises the run.
    expect(await screen.findByText(/Housekeeping complete — 2 jobs run/i)).toBeInTheDocument();
  });

  it("surfaces a per-job error and an error toast when a job fails", async () => {
    mswServer.use(
      http.get("/api/v1/housekeeping/status", () =>
        HttpResponse.json({ status: "ok", registered_jobs: 1 }),
      ),
      http.post("/api/v1/housekeeping/run", () =>
        HttpResponse.json({
          status: "ok",
          jobs: [{ name: "cleanup_snooze", error: "db timeout", duration_ms: 3 }],
          errors: 1,
        }),
      ),
    );
    setup();
    const user = userEvent.setup();
    await waitFor(() => expect(screen.getByText(/1 job registered/i)).toBeInTheDocument());

    await user.click(screen.getByRole("button", { name: "Run now" }));
    await user.click(await screen.findByRole("button", { name: "Run housekeeping" }));

    expect(await screen.findByText("db timeout")).toBeInTheDocument();
    expect(await screen.findByText(/Housekeeping finished with 1 job error/i)).toBeInTheDocument();
  });
});
