import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { authStore } from "@/lib/auth/store";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { CreateApiKeyForm } from "./CreateApiKeyForm";

beforeAll(() => {
  if (typeof window !== "undefined" && !window.ResizeObserver) {
    window.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

function login(permissions: string[]) {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({ sub: "x", exp: Math.floor(Date.now() / 1000) + 3600, permissions }),
  );
  authStore.getState().login(`${header}.${body}.sig`);
}

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

describe("CreateApiKeyForm permission picker", () => {
  beforeEach(() => {
    localStorage.clear();
    authStore.getState().logout();
    // usePermissionsCatalogue fires regardless of the wildcard branch; stub it
    // so the non-wildcard path doesn't hit MSW's catch-all handler.
    mswServer.use(
      http.get("/api/v1/permissions", () =>
        HttpResponse.json({ data: ["rw_all", "ro_rule", "rw_record", "ro_tenant", "rw_tenant"] }),
      ),
    );
  });
  afterEach(() => {
    localStorage.clear();
    authStore.getState().logout();
  });

  it("reuses the shared picker: offers the caller's own perms with descriptions, minus reserved tenant grants", async () => {
    // A non-wildcard caller: the picker is scoped to their own permissions.
    login(["ro_rule", "rw_record", "rw_tenant"]);
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <CreateApiKeyForm onDone={() => undefined} />
      </Wrapper>,
    );

    const combobox = await screen.findByRole("combobox", { name: /permissions/i });
    await user.click(combobox);

    // Own permissions are offered...
    const roRule = await screen.findByRole("option", { name: /ro_rule/ });
    expect(screen.getByRole("option", { name: /rw_record/ })).toBeInTheDocument();
    // ...carrying the same human descriptions the role editor shows (proving the
    // shared PermissionsCombobox, not the old label-only options).
    expect(roRule).toHaveTextContent(/read-only access to rules/i);
    // ...but the reserved tenant grant an API key may never carry is filtered out.
    expect(screen.queryByRole("option", { name: /rw_tenant/ })).not.toBeInTheDocument();
  });

  it("offers the full catalogue (minus reserved tenant grants) for an rw_all caller", async () => {
    login(["rw_all"]);
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <CreateApiKeyForm onDone={() => undefined} />
      </Wrapper>,
    );

    await user.click(await screen.findByRole("combobox", { name: /permissions/i }));
    expect(await screen.findByRole("option", { name: /rw_record/ })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: /ro_rule/ })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: /ro_tenant/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("option", { name: /rw_tenant/ })).not.toBeInTheDocument();
  });
});
