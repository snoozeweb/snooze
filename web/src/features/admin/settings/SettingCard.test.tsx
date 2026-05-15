import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { beforeAll, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import type { FormField } from "@/shared/forms/types";
import { SettingCard } from "./SettingCard";

// jsdom polyfill — Radix BubbleInput uses ResizeObserver inside Switch.
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

const switchField: FormField = {
  display_name: "Metrics enabled",
  component: "Switch",
  description: "Enable Prometheus metrics.",
  default_value: true,
  group: "general",
};

const stringField: FormField = {
  display_name: "LDAP host",
  component: "String",
  description: "LDAP server URL.",
  default_value: "",
  group: "ldap",
};

describe("SettingCard", () => {
  it("renders label, description, and a typed input", () => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <SettingCard
          field={switchField}
          name="metrics_enabled"
          initialValue={true}
          recordUid="s-1"
          onChange={() => {}}
        />
      </Wrapper>,
    );
    expect(screen.getByText("Metrics enabled")).toBeInTheDocument();
    expect(screen.getByText("Enable Prometheus metrics.")).toBeInTheDocument();
    expect(screen.getByRole("switch")).toBeInTheDocument();
  });

  it("disables Save and Reset when value matches initialValue", () => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <SettingCard
          field={stringField}
          name="ldap.host"
          initialValue="ldap://example.com"
          recordUid="s-2"
          onChange={() => {}}
        />
      </Wrapper>,
    );
    const save = screen.getByRole("button", { name: /^save$/i });
    const reset = screen.getByRole("button", { name: /^reset$/i });
    expect(save).toBeDisabled();
    expect(reset).toBeDisabled();
  });

  it("shows 'not set' indicator when no record exists yet", () => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <SettingCard
          field={stringField}
          name="ldap.host"
          initialValue={undefined}
          recordUid={undefined}
          onChange={() => {}}
        />
      </Wrapper>,
    );
    expect(screen.getByText(/not set/i)).toBeInTheDocument();
  });

  it("POSTs a new record when recordUid is undefined and Save is clicked", async () => {
    const bodies: Array<Record<string, unknown>> = [];
    mswServer.use(
      http.post("/api/v1/settings", async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>);
        return HttpResponse.json({ uid: "new-1", name: "ldap.host" });
      }),
    );
    const onChange = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <SettingCard
          field={stringField}
          name="ldap.host"
          initialValue={undefined}
          recordUid={undefined}
          onChange={onChange}
        />
      </Wrapper>,
    );
    // Type a value — find the String input by its display name label.
    const input = screen.getByLabelText("LDAP host");
    await user.type(input, "ldap://x");
    await user.click(screen.getByRole("button", { name: /^save$/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toMatchObject({ name: "ldap.host", value: "ldap://x" });
    await waitFor(() => expect(onChange).toHaveBeenCalled());
  });

  it("PATCHes an existing record when recordUid is defined and Save is clicked", async () => {
    const patched: Array<Record<string, unknown>> = [];
    mswServer.use(
      http.patch("/api/v1/settings/s-3", async ({ request }) => {
        patched.push((await request.json()) as Record<string, unknown>);
        return HttpResponse.json({ uid: "s-3", name: "ldap.host", value: "x" });
      }),
    );
    const onChange = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <SettingCard
          field={stringField}
          name="ldap.host"
          initialValue="old"
          recordUid="s-3"
          onChange={onChange}
        />
      </Wrapper>,
    );
    const input = screen.getByLabelText("LDAP host");
    await user.clear(input);
    await user.type(input, "new");
    await user.click(screen.getByRole("button", { name: /^save$/i }));
    await waitFor(() => expect(patched).toHaveLength(1));
    expect(patched[0]).toMatchObject({ value: "new" });
    await waitFor(() => expect(onChange).toHaveBeenCalled());
  });

  it("Reset returns the input to initialValue and re-disables both buttons", async () => {
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <SettingCard
          field={stringField}
          name="ldap.host"
          initialValue="ldap://example.com"
          recordUid="s-4"
          onChange={() => {}}
        />
      </Wrapper>,
    );
    const input = screen.getByLabelText<HTMLInputElement>("LDAP host");
    await user.type(input, "X");
    expect(input.value).toBe("ldap://example.comX");
    const reset = screen.getByRole("button", { name: /^reset$/i });
    expect(reset).not.toBeDisabled();
    await user.click(reset);
    expect(input.value).toBe("ldap://example.com");
    expect(reset).toBeDisabled();
    expect(screen.getByRole("button", { name: /^save$/i })).toBeDisabled();
  });

  it("Delete reverts a record by DELETEing it", async () => {
    let deleted = 0;
    mswServer.use(
      http.delete("/api/v1/settings/s-5", () => {
        deleted += 1;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const onChange = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <SettingCard
          field={stringField}
          name="ldap.host"
          initialValue="old"
          recordUid="s-5"
          onChange={onChange}
        />
      </Wrapper>,
    );
    await user.click(screen.getByRole("button", { name: /revert to default/i }));
    await waitFor(() => expect(deleted).toBe(1));
    await waitFor(() => expect(onChange).toHaveBeenCalled());
  });

  it("does not render the Delete button when no record exists yet", () => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <SettingCard
          field={stringField}
          name="ldap.host"
          initialValue={undefined}
          recordUid={undefined}
          onChange={() => {}}
        />
      </Wrapper>,
    );
    expect(screen.queryByRole("button", { name: /revert to default/i })).toBeNull();
  });
});
