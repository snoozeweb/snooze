import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi, beforeAll } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { TenantMatchEditor } from "./TenantMatchEditor";

beforeAll(() => {
  if (typeof window !== "undefined" && !window.ResizeObserver) {
    window.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

const TENANTS = [
  { id: "acme", display_name: "Acme Corp", status: "active" },
  { id: "default", display_name: "Default", status: "active" },
];

const TENANT_LIST_RESPONSE = {
  data: TENANTS,
  meta: { count: 2, limit: 200, offset: 0, total: 2 },
};

const EXISTING_RULE = {
  uid: "rule-1",
  match_type: "group",
  match: "ops-team",
  tenant_id: "acme",
  priority: 10,
};

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

describe("TenantMatchEditor — create mode", () => {
  it("renders match_type selector, match input, tenant picker, priority field", async () => {
    mswServer.use(http.get("/api/v1/tenant", () => HttpResponse.json(TENANT_LIST_RESPONSE)));
    const Wrapper = wrap();
    render(
      <Wrapper>
        <TenantMatchEditor uid={undefined} onClose={vi.fn()} />
      </Wrapper>,
    );
    // Match type label appears
    await waitFor(() => expect(screen.getByText("Match type")).toBeInTheDocument());
    // Match input rendered with default label
    expect(screen.getByLabelText("Group name (case-insensitive)")).toBeInTheDocument();
    // Priority field
    expect(screen.getByLabelText("Priority")).toBeInTheDocument();
    // Tenant picker label
    expect(screen.getByText("Target tenant")).toBeInTheDocument();
  });

  it("creates a rule: POST is called with correct body on submit", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.get("/api/v1/tenant", () => HttpResponse.json(TENANT_LIST_RESPONSE)),
      http.post("/api/v1/tenant_match", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json(
          {
            uid: "new-rule",
            match_type: "group",
            match: "dev-team",
            tenant_id: "acme",
          },
          { status: 201 },
        );
      }),
    );
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <TenantMatchEditor uid={undefined} onClose={vi.fn()} />
      </Wrapper>,
    );

    // Type into match input
    await user.type(await screen.findByLabelText("Group name (case-insensitive)"), "dev-team");

    // Select tenant via the select
    // Open the target tenant trigger and pick acme
    const tenantTrigger = await screen.findByRole("combobox", { name: /target tenant/i });
    await user.click(tenantTrigger);
    await user.click(await screen.findByRole("option", { name: /acme/i }));

    await user.click(screen.getByRole("button", { name: /create/i }));

    await waitFor(() => expect(bodies).toHaveLength(1));
    expect((bodies[0] as { match?: string }).match).toBe("dev-team");
    expect((bodies[0] as { tenant_id?: string }).tenant_id).toBe("acme");
    expect((bodies[0] as { match_type?: string }).match_type).toBe("group");
  });

  it("surfaces 409 conflict: shows the conflict message in a toast", async () => {
    mswServer.use(
      http.get("/api/v1/tenant", () => HttpResponse.json(TENANT_LIST_RESPONSE)),
      http.post("/api/v1/tenant_match", () =>
        HttpResponse.json(
          { code: "conflict", detail: "tenant_match: a rule for (group, ops-team) already exists" },
          { status: 409 },
        ),
      ),
    );
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <TenantMatchEditor uid={undefined} onClose={vi.fn()} />
      </Wrapper>,
    );

    await user.type(await screen.findByLabelText("Group name (case-insensitive)"), "ops-team");
    const tenantTrigger = await screen.findByRole("combobox", { name: /target tenant/i });
    await user.click(tenantTrigger);
    await user.click(await screen.findByRole("option", { name: /acme/i }));

    await user.click(screen.getByRole("button", { name: /create/i }));

    await waitFor(() =>
      expect(
        screen.getByText("A rule for this (match type, match) pair already exists."),
      ).toBeInTheDocument(),
    );
  });

  it("blocks submit when match value is empty (EditorAbort path)", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.get("/api/v1/tenant", () => HttpResponse.json(TENANT_LIST_RESPONSE)),
      http.post("/api/v1/tenant_match", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({}, { status: 201 });
      }),
    );
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <TenantMatchEditor uid={undefined} onClose={vi.fn()} />
      </Wrapper>,
    );
    await screen.findByLabelText("Group name (case-insensitive)");
    // Don't fill in match — just submit
    await user.click(screen.getByRole("button", { name: /create/i }));
    // POST should NOT have been called
    expect(bodies).toHaveLength(0);
  });

  it("blocks submit when tenant_id is empty (EditorAbort path)", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.get("/api/v1/tenant", () => HttpResponse.json(TENANT_LIST_RESPONSE)),
      http.post("/api/v1/tenant_match", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({}, { status: 201 });
      }),
    );
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <TenantMatchEditor uid={undefined} onClose={vi.fn()} />
      </Wrapper>,
    );
    // Fill match but not tenant
    await user.type(await screen.findByLabelText("Group name (case-insensitive)"), "dev-team");
    await user.click(screen.getByRole("button", { name: /create/i }));
    expect(bodies).toHaveLength(0);
  });

  it("changing match_type updates the match input label", async () => {
    mswServer.use(http.get("/api/v1/tenant", () => HttpResponse.json(TENANT_LIST_RESPONSE)));
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <TenantMatchEditor uid={undefined} onClose={vi.fn()} />
      </Wrapper>,
    );
    // Initially shows Group label
    await waitFor(() =>
      expect(screen.getByLabelText("Group name (case-insensitive)")).toBeInTheDocument(),
    );

    // Change to domain
    const matchTypeTrigger = screen.getByRole("combobox", { name: /match type/i });
    await user.click(matchTypeTrigger);
    await user.click(await screen.findByRole("option", { name: /domain/i }));

    await waitFor(() =>
      expect(screen.getByLabelText("Email domain (e.g. example.com)")).toBeInTheDocument(),
    );
  });
});

describe("TenantMatchEditor — edit mode", () => {
  it("shows edit title and pre-fills match from existing rule", async () => {
    mswServer.use(
      http.get("/api/v1/tenant", () => HttpResponse.json(TENANT_LIST_RESPONSE)),
      http.get("/api/v1/tenant_match/rule-1", () => HttpResponse.json(EXISTING_RULE)),
      http.patch("/api/v1/tenant_match/rule-1", () => HttpResponse.json(EXISTING_RULE)),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <TenantMatchEditor uid="rule-1" onClose={vi.fn()} />
      </Wrapper>,
    );

    // Edit mode: title is "Edit routing rule" and Save button present
    expect(await screen.findByText("Edit routing rule")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /save/i })).toBeInTheDocument();

    // Wait for form to load with existing match value
    const matchInput = await screen.findByLabelText("Group name (case-insensitive)");
    await waitFor(() => expect((matchInput as HTMLInputElement).value).toBe("ops-team"));
  });

  it("calls PATCH on submit in edit mode", async () => {
    const patches: unknown[] = [];
    mswServer.use(
      http.get("/api/v1/tenant", () => HttpResponse.json(TENANT_LIST_RESPONSE)),
      http.get("/api/v1/tenant_match/rule-1", () => HttpResponse.json(EXISTING_RULE)),
      http.patch("/api/v1/tenant_match/rule-1", async ({ request }) => {
        patches.push(await request.json());
        return HttpResponse.json(EXISTING_RULE);
      }),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <TenantMatchEditor uid="rule-1" onClose={onClose} />
      </Wrapper>,
    );

    // Wait for form to be pre-filled
    const matchInput = await screen.findByLabelText("Group name (case-insensitive)");
    await waitFor(() => expect((matchInput as HTMLInputElement).value).toBe("ops-team"));

    // Type a different match value to make the form dirty
    await user.clear(matchInput);
    await user.type(matchInput, "engineering");

    // Explicitly (re-)select the tenant to ensure tenant_id is set in form state.
    // The Radix Select controlled value may not write back through RHF on reset
    // in jsdom, so we drive it via user interaction like the create-mode test.
    const tenantTrigger = screen.getByRole("combobox", { name: /target tenant/i });
    await user.click(tenantTrigger);
    await user.click(await screen.findByRole("option", { name: /acme/i }));

    // Click the Save button (external button targeting the form via the `form` attribute)
    await user.click(screen.getByRole("button", { name: /save/i }));

    // After submit, PATCH should be called and onClose should fire (success path)
    await waitFor(() => expect(patches).toHaveLength(1), { timeout: 3000 });
    expect((patches[0] as { match?: string }).match).toBe("engineering");
    expect(onClose).toHaveBeenCalled();
  });
});
