import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { beforeAll, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { NotificationEditor } from "./NotificationEditor";

// jsdom polyfill — Radix BubbleInput uses ResizeObserver inside Drawer.
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

describe("NotificationEditor", () => {
  it("creates a new notification on Save", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/notification", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "n-new", name: "x" });
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
        <NotificationEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "page-on-call");
    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect((bodies[0] as { name: string }).name).toBe("page-on-call");
    expect(onClose).toHaveBeenCalled();
  });
});
