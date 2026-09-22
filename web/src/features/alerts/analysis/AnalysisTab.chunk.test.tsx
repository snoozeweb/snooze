// The one AnalysisTab case that needs the editor module itself to be broken:
// a lazy chunk that 404s after a deploy. It lives in its own file because the
// only way to break a dynamic import is to mock the module, and that mock
// would otherwise apply to every test in AnalysisTab.test.tsx — all of which
// need the real editor.
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { authStore } from "@/lib/auth/store";
import { mswServer } from "@/tests/msw/server";
import { AnalysisTab } from "./AnalysisTab";

// The failure a browser reports when the chunk the running bundle asks for is
// no longer on the server.
vi.mock("./AnalysisEditor", () => {
  throw new Error("Failed to fetch dynamically imported module: /assets/AnalysisEditor.js");
});

function loginWithPerms(perms: string[]) {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({
      sub: "tester",
      exp: Math.floor(Date.now() / 1000) + 3600,
      permissions: perms,
    }),
  );
  authStore.getState().login(`${header}.${body}.sig`);
}

describe("AnalysisTab with an unreachable editor chunk", () => {
  let consoleError: ReturnType<typeof vi.spyOn>;
  beforeEach(() => {
    consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
  });
  afterEach(() => {
    consoleError.mockRestore();
    authStore.getState().logout({ revoke: false });
  });

  it("contains the failure in the tab instead of letting it take the route down", async () => {
    const user = userEvent.setup();
    loginWithPerms(["ro_record", "rw_protected"]);
    mswServer.use(
      http.get("/api/v1/record/r1/agentic", () =>
        HttpResponse.json(
          { error: { code: "not_found", message: "record carries no analysis" } },
          { status: 404 },
        ),
      ),
    );
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <TooltipProvider>
          <AnalysisTab uid="r1" />
        </TooltipProvider>
      </QueryClientProvider>,
    );

    await user.click(await screen.findByRole("button", { name: "Write analysis" }));

    // The tab says what happened and offers the only thing that fixes it,
    // and the surrounding tree is still standing.
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "The analysis editor could not be loaded.",
    );
    expect(screen.getByRole("button", { name: "Reload" })).toBeInTheDocument();
    expect(screen.getByText("Editing")).toBeInTheDocument();
  });
});
