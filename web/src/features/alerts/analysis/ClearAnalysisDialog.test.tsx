import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { ClearAnalysisDialog } from "./ClearAnalysisDialog";

function makeWrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe("ClearAnalysisDialog", () => {
  afterEach(cleanup);

  it("says what is lost and what can still happen", () => {
    render(<ClearAnalysisDialog uid="r1" open onOpenChange={() => undefined} />, {
      wrapper: makeWrapper(),
    });
    expect(screen.getByText("Remove this analysis?")).toBeInTheDocument();
    expect(
      screen.getByText(
        "This cannot be undone. The alert-rca agent loop may analyse the alert again later.",
      ),
    ).toBeInTheDocument();
    // The quiet way out names what it preserves, not "Cancel".
    expect(screen.getByRole("button", { name: "Keep analysis" })).toBeInTheDocument();
  });

  it("DELETEs the analysis, then closes and tells its parent", async () => {
    const user = userEvent.setup();
    let deleted = 0;
    mswServer.use(
      http.delete("/api/v1/record/r1/agentic", () => {
        deleted += 1;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const onOpenChange = vi.fn();
    const onCleared = vi.fn();
    render(
      <ClearAnalysisDialog uid="r1" open onOpenChange={onOpenChange} onCleared={onCleared} />,
      { wrapper: makeWrapper() },
    );

    await user.click(screen.getByRole("button", { name: "Remove analysis" }));
    await waitFor(() => expect(deleted).toBe(1));
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(onCleared).toHaveBeenCalledTimes(1);
  });

  it("stays open with the server's reason when the delete is refused", async () => {
    const user = userEvent.setup();
    mswServer.use(
      http.delete("/api/v1/record/r1/agentic", () =>
        HttpResponse.json(
          { error: { code: "forbidden", message: "rw_protected required" } },
          { status: 403 },
        ),
      ),
    );
    const onOpenChange = vi.fn();
    render(<ClearAnalysisDialog uid="r1" open onOpenChange={onOpenChange} />, {
      wrapper: makeWrapper(),
    });

    await user.click(screen.getByRole("button", { name: "Remove analysis" }));
    expect(await screen.findByText(/rw_protected required/i)).toBeInTheDocument();
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });
});
