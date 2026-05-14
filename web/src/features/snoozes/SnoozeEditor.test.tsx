import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { SnoozeEditor } from "./SnoozeEditor";

beforeAll(() => {
  if (typeof window !== "undefined" && !window.ResizeObserver) {
    window.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <TooltipProvider delay={0}>
        <ToastProvider>
          {children}
          <Toaster />
        </ToastProvider>
      </TooltipProvider>
    </QueryClientProvider>
  );
}

describe("SnoozeEditor", () => {
  it("creates a new snooze on Save", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/snooze", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "s-new", name: "x" });
      }),
      http.get("/api/v1/record", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <SnoozeEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "quiet-friday");
    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect((bodies[0] as { name: string }).name).toBe("quiet-friday");
  });

  it("shows the Diff section in edit mode", async () => {
    mswServer.use(
      http.get("/api/v1/snooze/sn1", () =>
        HttpResponse.json({
          uid: "sn1",
          name: "quiet-friday",
          enabled: true,
          condition: { type: "ALWAYS_TRUE" },
          ttl: 3600,
        }),
      ),
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <SnoozeEditor uid="sn1" onClose={() => undefined} />
      </Wrapper>,
    );
    expect(await screen.findByRole("button", { name: /^diff/i })).toBeInTheDocument();
  });

  it("hides the Diff section when creating", () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <SnoozeEditor uid={undefined} onClose={() => undefined} />
      </Wrapper>,
    );
    expect(screen.queryByRole("button", { name: /^diff/i })).not.toBeInTheDocument();
  });
});
