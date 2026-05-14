import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { EnvironmentEditor } from "./EnvironmentEditor";

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

describe("EnvironmentEditor", () => {
  it("creates a new environment on Create", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/environment", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "e-new", name: "staging" });
      }),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <EnvironmentEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "staging");
    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect((bodies[0] as { name: string }).name).toBe("staging");
  });
});
