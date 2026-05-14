import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { beforeAll, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { SettingEditor } from "./SettingEditor";

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

describe("SettingEditor", () => {
  it("creates a new setting with valid JSON on Create", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/settings", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "s-new", name: "max_retries" });
      }),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <SettingEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "max_retries");
    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect((bodies[0] as { name: string }).name).toBe("max_retries");
    expect(onClose).toHaveBeenCalled();
  });

  it("blocks submit when JSON is invalid", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/settings", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "s-new", name: "x" });
      }),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <SettingEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "my_setting");
    // Find the JSON value textarea (the one with rows=8, last of all textboxes)
    const allTextareas = screen.getAllByRole("textbox");
    const jsonTextarea = allTextareas.at(-1)!;
    await user.clear(jsonTextarea);
    await user.type(jsonTextarea, "not valid json");
    await user.click(screen.getByRole("button", { name: /create/i }));
    // The error message should appear and no POST should have been made
    await waitFor(() =>
      expect(screen.getByText(/invalid json|unexpected token/i)).toBeInTheDocument(),
    );
    expect(bodies).toHaveLength(0);
    expect(onClose).not.toHaveBeenCalled();
  });
});
