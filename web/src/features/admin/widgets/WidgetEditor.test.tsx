import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { beforeAll, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { WidgetEditor } from "./WidgetEditor";

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

describe("WidgetEditor", () => {
  it("creates a new widget with valid JSON config on Save", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/widget", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "w-new", name: "x" });
      }),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <WidgetEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "patlite-floor1");
    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect((bodies[0] as { name: string }).name).toBe("patlite-floor1");
    expect(onClose).toHaveBeenCalled();
  });

  it("blocks submit when JSON config is invalid", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/widget", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "w-new", name: "x" });
      }),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <WidgetEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "patlite-floor1");
    // Find the JSON config textarea (the one with rows=10, last of all textboxes)
    const allTextareas = screen.getAllByRole("textbox");
    const jsonTextarea = allTextareas.at(-1)!;
    await user.clear(jsonTextarea);
    await user.type(jsonTextarea, "not valid json");
    await user.click(screen.getByRole("button", { name: /create/i }));
    // The error message should appear and no POST should have been made
    await waitFor(() =>
      expect(
        screen.getByText(/invalid json|expected a json object|unexpected token/i),
      ).toBeInTheDocument(),
    );
    expect(bodies).toHaveLength(0);
    expect(onClose).not.toHaveBeenCalled();
  });
});
