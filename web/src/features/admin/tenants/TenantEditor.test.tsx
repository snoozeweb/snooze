// web/src/features/admin/tenants/TenantEditor.test.tsx
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi, beforeAll, beforeEach } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { TenantEditor } from "./TenantEditor";

// Convenience: suppress clipboard errors in jsdom
Object.defineProperty(navigator, "clipboard", {
  value: { writeText: vi.fn().mockResolvedValue(undefined) },
  configurable: true,
});

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

describe("TenantEditor create — admin provisioning", () => {
  beforeEach(() => {
    mswServer.use(
      http.post("/api/v1/tenant", async ({ request }) => {
        // Capture the body to return the admin block
        await request.json();
        return HttpResponse.json(
          {
            added: ["acme"],
            admin: {
              username: "admin",
              password: "PWPWPWPWPWPWPWPWPWPWPWPWPWPWPWPW",
              method: "local",
              created: true,
            },
          },
          { status: 201 },
        );
      }),
    );
  });

  it("shows create-admin checkbox (checked) and admin-username field in create mode", async () => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <TenantEditor id={undefined} onClose={vi.fn()} />
      </Wrapper>,
    );
    expect(await screen.findByLabelText(/create admin user/i)).toBeChecked();
    expect(screen.getByLabelText(/admin username/i)).toBeInTheDocument();
  });

  it("shows the one-time credential reveal after create returns an admin block", async () => {
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <TenantEditor id={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(await screen.findByLabelText(/slug/i), "acme");
    await user.type(screen.getByLabelText(/display name/i), "Acme");
    await user.click(screen.getByRole("button", { name: /create/i }));
    expect(await screen.findByText("PWPWPWPWPWPWPWPWPWPWPWPWPWPWPWPW")).toBeInTheDocument();
    // onClose must NOT be called yet — drawer should remain open until dialog is dismissed
    expect(onClose).not.toHaveBeenCalled();
  });
});

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

describe("TenantEditor edit — listed toggle", () => {
  it("toggling Listed sends listed in the update body", async () => {
    mswServer.use(
      http.get("/api/v1/tenant/acme", () =>
        HttpResponse.json({
          id: "acme",
          display_name: "Acme Corp",
          status: "active",
          listed: true,
        }),
      ),
    );
    const bodies: unknown[] = [];
    mswServer.use(
      http.patch("/api/v1/tenant/acme", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({
          id: "acme",
          display_name: "Acme Corp",
          status: "active",
          listed: false,
        });
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
    // Wait for form to load
    await waitFor(() =>
      expect(screen.getByLabelText<HTMLInputElement>(/display name/i).value).toBe("Acme Corp"),
    );
    // The Listed checkbox should be checked (listed:true)
    const listedCheckbox = screen.getByRole("checkbox", { name: /listed/i });
    expect(listedCheckbox).toBeChecked();
    // Toggle it off
    await user.click(listedCheckbox);
    expect(listedCheckbox).not.toBeChecked();
    // Save
    await user.click(screen.getByRole("button", { name: /save/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect((bodies[0] as { listed?: boolean }).listed).toBe(false);
  });
});

describe("TenantEditor edit — login link block", () => {
  it("shows the login link and a Rotate action when login_key is present", async () => {
    mswServer.use(
      http.get("/api/v1/tenant/acme", () =>
        HttpResponse.json({
          id: "acme",
          display_name: "Acme Corp",
          status: "active",
          login_key: "KEY-abc",
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <TenantEditor id="acme" onClose={vi.fn()} />
      </Wrapper>,
    );
    // Wait for form to load
    await waitFor(() =>
      expect(screen.getByLabelText<HTMLInputElement>(/display name/i).value).toBe("Acme Corp"),
    );
    // The readonly input should show the login link value
    expect(screen.getByDisplayValue(/\/web\/login\?key=KEY-abc/)).toBeInTheDocument();
    // A Rotate button must be visible
    expect(screen.getByRole("button", { name: /rotate/i })).toBeInTheDocument();
  });

  it("shows 'Generate login link' button when login_key is absent", async () => {
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
      expect(screen.getByLabelText<HTMLInputElement>(/display name/i).value).toBe("Acme Corp"),
    );
    expect(screen.getByRole("button", { name: /generate login link/i })).toBeInTheDocument();
  });

  it("calls useRotateLoginKey when Rotate is clicked and confirmed", async () => {
    let rotateCalls = 0;
    mswServer.use(
      http.get("/api/v1/tenant/acme", () =>
        HttpResponse.json({
          id: "acme",
          display_name: "Acme Corp",
          status: "active",
          login_key: "KEY-abc",
        }),
      ),
      http.post("/api/v1/tenant/acme/rotate-login-key", () => {
        rotateCalls++;
        return HttpResponse.json({ id: "acme", login_key: "NEW-KEY" });
      }),
    );
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <TenantEditor id="acme" onClose={vi.fn()} />
      </Wrapper>,
    );
    await waitFor(() =>
      expect(screen.getByLabelText<HTMLInputElement>(/display name/i).value).toBe("Acme Corp"),
    );
    await user.click(screen.getByRole("button", { name: /rotate/i }));
    // Confirm dialog should appear
    expect(screen.getByRole("button", { name: /rotate login key/i })).toBeInTheDocument();
    // Confirm
    await user.click(screen.getByRole("button", { name: /rotate login key/i }));
    // The rotate endpoint must actually be hit (guards against a silently
    // throwing handler that would otherwise let this test pass vacuously).
    await waitFor(() => expect(rotateCalls).toBe(1));
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
      expect(screen.getByLabelText<HTMLInputElement>(/display name/i).value).toBe("Acme Corp"),
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

  it("shows a confirmation dialog before deleting a non-default tenant", async () => {
    let deleteCalls = 0;
    mswServer.use(
      http.get("/api/v1/tenant/acme", () =>
        HttpResponse.json({ id: "acme", display_name: "Acme Corp", status: "active" }),
      ),
      http.delete("/api/v1/tenant/acme", () => {
        deleteCalls++;
        return HttpResponse.json({ deleted: ["acme"] });
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
      expect(screen.getByLabelText<HTMLInputElement>(/slug/i).value).toBe("acme"),
    );
    // Click the danger button — must not delete yet (confirmation required)
    await user.click(screen.getByRole("button", { name: /delete tenant/i }));
    expect(deleteCalls).toBe(0);
    // The confirmation dialog must appear with irreversibility copy
    expect(screen.getByText(/cannot be undone/i)).toBeInTheDocument();
    // Confirm
    await user.click(screen.getByRole("button", { name: /^delete tenant$/i }));
    await waitFor(() => expect(deleteCalls).toBe(1));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });
});
