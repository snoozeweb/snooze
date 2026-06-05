// web/src/features/admin/tenants/TenantEditor.test.tsx
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi, beforeAll } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { TenantEditor } from "./TenantEditor";

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

describe("TenantEditor create", () => {
  it("renders a slug field and a display_name field", async () => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <TenantEditor id={undefined} onClose={vi.fn()} />
      </Wrapper>,
    );
    expect(await screen.findByLabelText(/slug/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/display name/i)).toBeInTheDocument();
  });

  it("POSTs a new tenant on Create", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/tenant", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json(
          { id: "acme", display_name: "Acme Corp", status: "active" },
          { status: 201 },
        );
      }),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <TenantEditor id={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(await screen.findByLabelText(/slug/i), "acme");
    await user.type(screen.getByLabelText(/display name/i), "Acme Corp");
    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    const sent = bodies[0] as { id: string; display_name: string; status: string };
    expect(sent.id).toBe("acme");
    expect(sent.display_name).toBe("Acme Corp");
    expect(sent.status).toBe("active");
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });
});

describe("TenantEditor edit", () => {
  it("loads the existing tenant and pre-fills the form", async () => {
    mswServer.use(
      http.get("/api/v1/tenant/acme", () =>
        HttpResponse.json({ id: "acme", display_name: "Acme Corp", status: "active" }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <TenantEditor id="acme" onClose={vi.fn()} />
      </Wrapper>,
    );
    await waitFor(() =>
      expect(
        screen.getByLabelText<HTMLInputElement>(/display name/i).value,
      ).toBe("Acme Corp"),
    );
    // Slug field is read-only in edit mode (id is immutable).
    expect(screen.getByLabelText<HTMLInputElement>(/slug/i).disabled).toBe(true);
  });

  it("PATCHes the tenant on Save", async () => {
    mswServer.use(
      http.get("/api/v1/tenant/acme", () =>
        HttpResponse.json({ id: "acme", display_name: "Acme Corp", status: "active" }),
      ),
    );
    const bodies: unknown[] = [];
    mswServer.use(
      http.patch("/api/v1/tenant/acme", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ id: "acme", display_name: "Updated", status: "active" });
      }),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <TenantEditor id="acme" onClose={onClose} />
      </Wrapper>,
    );
    await waitFor(() =>
      expect(screen.getByLabelText<HTMLInputElement>(/display name/i).value).toBe("Acme Corp"),
    );
    await user.clear(screen.getByLabelText(/display name/i));
    await user.type(screen.getByLabelText(/display name/i), "Updated");
    await user.click(screen.getByRole("button", { name: /save/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect((bodies[0] as { display_name?: string }).display_name).toBe("Updated");
  });

  it("blocks deleting the 'default' tenant (delete button disabled)", async () => {
    mswServer.use(
      http.get("/api/v1/tenant/default", () =>
        HttpResponse.json({ id: "default", display_name: "Default", status: "active" }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <TenantEditor id="default" onClose={vi.fn()} />
      </Wrapper>,
    );
    await waitFor(() =>
      expect(screen.getByLabelText<HTMLInputElement>(/slug/i).value).toBe("default"),
    );
    // The reserved "default" tenant must not have an enabled delete action.
    const deleteBtn = screen.queryByRole("button", { name: /delete/i });
    if (deleteBtn) {
      expect(deleteBtn).toBeDisabled();
    }
    // If there is no delete button at all for "default" that is also acceptable.
  });
});
