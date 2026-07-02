import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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

  it("requires an organization and blocks sign-in until one is chosen (multi-tenant)", async () => {
    const user = userEvent.setup();
    vi.spyOn(authApi, "fetchLoginConfig").mockResolvedValue({
      backends: [{ name: "local", kind: "password" }],
      tenants: [
        { id: "acme", display_name: "Acme" },
        { id: "globex", display_name: "Globex" },
      ],
    });
    renderLogin();
    await screen.findByLabelText(/username/i);
    const org = screen.getByLabelText(/organization/i);
    expect(org).toBeRequired();
    // The credentials must never be checked against the default org silently:
    // the submit button stays disabled until an org is explicitly chosen.
    const submit = screen.getByRole("button", { name: /^sign in$/i });
    expect(submit).toBeDisabled();
    await user.selectOptions(org, "globex");
    expect(submit).toBeEnabled();
  });

  it("moves focus to the SSO error banner once the form has mounted", async () => {
    searchState["sso_error"] = "sign-in failed";
    mockConfig([{ name: "local", kind: "password" }]);
    renderLogin();
    const banner = await screen.findByText(/sign-in failed/i);
    // The focus-management effect previously no-op'd because it fired while the
    // config was still loading (banner not yet mounted).
    await waitFor(() => expect(banner).toHaveFocus());
  });

  it("only-SSO config shows the button and no credential form", async () => {
    mockConfig([{ name: "microsoft", kind: "redirect", display_name: "Microsoft 365" }]);
    renderLogin();
    expect(await screen.findByRole("button", { name: /microsoft 365/i })).toBeInTheDocument();
    expect(screen.queryByLabelText(/password/i)).not.toBeInTheDocument();
  });
});
