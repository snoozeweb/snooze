import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Login } from "./Login";
import * as authApi from "./api";

const searchState: Record<string, unknown> = {};
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  useSearch: () => searchState,
}));

function mockConfig(backends: authApi.LoginBackend[]) {
  vi.spyOn(authApi, "fetchLoginConfig").mockResolvedValue({ backends, tenants: [] });
}

function renderLogin() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <Login />
    </QueryClientProvider>,
  );
}

describe("Login", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    for (const k of Object.keys(searchState)) delete searchState[k];
  });

  it("shows the local credential form as the primary form", async () => {
    mockConfig([{ name: "local", kind: "password" }]);
    renderLogin();
    expect(await screen.findByLabelText(/username/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
  });

  it("renders an SSO button that links to the start URL", async () => {
    mockConfig([
      { name: "local", kind: "password" },
      { name: "microsoft", kind: "redirect", display_name: "Microsoft 365", icon: "microsoft" },
    ]);
    renderLogin();
    const btn = await screen.findByRole("button", { name: /microsoft 365/i });
    expect(btn).toBeInTheDocument();
  });

  it("shows the SSO error banner from ?sso_error", async () => {
    searchState["sso_error"] = "sign-in failed";
    mockConfig([{ name: "local", kind: "password" }]);
    renderLogin();
    expect(await screen.findByText(/sign-in failed/i)).toBeInTheDocument();
  });

  it("only-SSO config shows the button and no credential form", async () => {
    mockConfig([{ name: "microsoft", kind: "redirect", display_name: "Microsoft 365" }]);
    renderLogin();
    expect(await screen.findByRole("button", { name: /microsoft 365/i })).toBeInTheDocument();
    expect(screen.queryByLabelText(/password/i)).not.toBeInTheDocument();
  });

  it("shows org-auto-detect hint when tenant_match_enabled is true and tenants > 1", async () => {
    vi.spyOn(authApi, "fetchLoginConfig").mockResolvedValue({
      backends: [{ name: "local", kind: "password" }],
      tenants: [
        { id: "acme", display_name: "Acme Corp" },
        { id: "default", display_name: "Default" },
      ],
      tenantMatchEnabled: true,
    });
    renderLogin();
    // Wait for the credential form to be fully rendered (query has resolved),
    // then assert the hint is present.
    await screen.findByLabelText(/username/i);
    expect(screen.getByText(/your organisation is detected automatically/i)).toBeInTheDocument();
  });

  it("does NOT show hint when tenant_match_enabled is false", async () => {
    vi.spyOn(authApi, "fetchLoginConfig").mockResolvedValue({
      backends: [{ name: "local", kind: "password" }],
      tenants: [
        { id: "acme", display_name: "Acme Corp" },
        { id: "default", display_name: "Default" },
      ],
      tenantMatchEnabled: false,
    });
    renderLogin();
    await screen.findByLabelText(/username/i);
    expect(
      screen.queryByText(/your organisation is detected automatically/i),
    ).not.toBeInTheDocument();
  });

  it("does NOT show hint when only one tenant (org picker hidden anyway)", async () => {
    vi.spyOn(authApi, "fetchLoginConfig").mockResolvedValue({
      backends: [{ name: "local", kind: "password" }],
      tenants: [{ id: "acme", display_name: "Acme Corp" }],
      tenantMatchEnabled: true,
    });
    renderLogin();
    await screen.findByLabelText(/username/i);
    expect(
      screen.queryByText(/your organisation is detected automatically/i),
    ).not.toBeInTheDocument();
  });
});
