import { describe, expect, it, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { authStore } from "@/lib/auth/store";

const navigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
}));

import { LoginCallback } from "./LoginCallback";

// LoginCallback renders <Logo />, which calls useConsoleConfig() (a TanStack
// Query hook), so a QueryClientProvider must wrap the tree.
function renderCallback() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <LoginCallback />
    </QueryClientProvider>,
  );
}

describe("LoginCallback", () => {
  beforeEach(() => {
    navigate.mockReset();
    vi.spyOn(authStore.getState(), "login").mockImplementation(() => {});
  });

  it("stores the token from the fragment and navigates to return_to", async () => {
    window.location.hash = "#token=jwt123&refresh_token=rt456&return_to=%2Fweb%2Frules";
    renderCallback();
    await vi.waitFor(() => {
      expect(authStore.getState().login).toHaveBeenCalledWith("jwt123", "rt456");
      expect(navigate).toHaveBeenCalledWith({ to: "/web/rules" });
    });
  });

  it("redirects to /web/login when no token is present", async () => {
    window.location.hash = "#oops=1";
    renderCallback();
    await vi.waitFor(() => {
      expect(navigate).toHaveBeenCalledWith({ to: "/web/login" });
    });
  });
});
