import { useState } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { mswServer } from "@/tests/msw/server";
import { SearchBar, type SearchBarChange } from "./SearchBar";

function setup(initial = "") {
  const onChange = vi.fn<(c: SearchBarChange) => void>();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  function Wrapper() {
    const [value, setValue] = useState(initial);
    return (
      <QueryClientProvider client={client}>
        <SearchBar
          value={value}
          onChange={(c) => {
            setValue(c.text);
            onChange(c);
          }}
        />
      </QueryClientProvider>
    );
  }
  render(<Wrapper />);
  return { onChange, getInput: () => screen.getByRole("textbox") };
}

const DEFAULT_FIELDS_HANDLER = http.get("/api/v1/condition/fields", () =>
  HttpResponse.json({
    data: [
      { name: "host", type: "string", description: "Host" },
      { name: "severity", type: "string", values: ["critical", "warning", "info"] },
      { name: "state", type: "string", values: ["open", "ack", "close"] },
    ],
  }),
);

describe("SearchBar", () => {
  it("renders a searchbox with the configured aria label", () => {
    mswServer.use(DEFAULT_FIELDS_HANDLER);
    setup();
    expect(screen.getByRole("textbox", { name: "Search" })).toBeInTheDocument();
  });

  it("shows the placeholder until the user types", () => {
    mswServer.use(DEFAULT_FIELDS_HANDLER);
    setup();
    // Placeholder lives in the overlay since the real input is transparent.
    expect(screen.getByText(/host = …/)).toBeInTheDocument();
  });

  it("calls onChange with the typed text", async () => {
    const user = userEvent.setup();
    mswServer.use(DEFAULT_FIELDS_HANDLER);
    const { onChange, getInput } = setup();
    await user.type(getInput(), "host");
    expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ text: "host" }));
  });

  it("opens an autocomplete popover with field suggestions when focused", async () => {
    const user = userEvent.setup();
    mswServer.use(DEFAULT_FIELDS_HANDLER);
    const { getInput } = setup();
    await user.click(getInput());
    // Wait for fields query to resolve.
    await waitFor(() => expect(screen.getByRole("listbox")).toBeInTheDocument());
    expect(screen.getByText("host")).toBeInTheDocument();
    expect(screen.getByText("severity")).toBeInTheDocument();
  });

  it("offers enum values after a field and operator", async () => {
    const user = userEvent.setup();
    mswServer.use(
      DEFAULT_FIELDS_HANDLER,
      http.post("/api/v1/condition/parse", () =>
        HttpResponse.json({ error: { pos: 9, message: "expected literal" } }),
      ),
    );
    const { getInput } = setup();
    await user.click(getInput());
    await user.type(getInput(), "state = ");
    await waitFor(() => {
      expect(screen.getByText("open")).toBeInTheDocument();
      expect(screen.getByText("ack")).toBeInTheDocument();
      expect(screen.getByText("close")).toBeInTheDocument();
    });
  });

  it("posts to /condition/parse and surfaces the server error", async () => {
    const user = userEvent.setup();
    mswServer.use(
      DEFAULT_FIELDS_HANDLER,
      http.post("/api/v1/condition/parse", () =>
        HttpResponse.json({ error: { pos: 7, token: "=", message: "expected literal" } }),
      ),
    );
    const { getInput } = setup();
    await user.type(getInput(), "host = ");
    // Wait for the 250ms debounce + the response.
    await waitFor(() => expect(screen.getByText(/expected literal/)).toBeInTheDocument(), {
      timeout: 1500,
    });
  });

  it("emits the parsed condition on a valid query", async () => {
    const user = userEvent.setup();
    mswServer.use(
      DEFAULT_FIELDS_HANDLER,
      http.post("/api/v1/condition/parse", () =>
        HttpResponse.json({ condition: { op: "=", field: "host", value: "srv-1" } }),
      ),
    );
    const { onChange, getInput } = setup();
    await user.type(getInput(), "host = srv-1");
    await waitFor(
      () => {
        const calls = onChange.mock.calls.map((c) => c[0]);
        const withCond = calls.find((c) => c.condition !== null);
        expect(withCond?.condition).toEqual({ op: "=", field: "host", value: "srv-1" });
      },
      { timeout: 1500 },
    );
  });

  it("drops a stale parse response that arrives after the user has typed more", async () => {
    // Regression for "characters get deleted when typing fast": the
    // debounced parse for value="ab" used to resolve and call onChange
    // with text="ab", overwriting the parent state that had already moved
    // on to "abc". The cleanup function's `cancelled` flag must
    // short-circuit the stale .then() callback.
    const user = userEvent.setup();

    // Hold the first parse response so we control exactly when it resolves.
    // `resolveFirst` starts as a no-op and gets swapped to the real resolver
    // inside the Promise constructor on the first call. The explicit no-op
    // initial value sidesteps a control-flow narrowing quirk in strict TS
    // that turned a `let foo: ... | null = null` into `never` at the call site.
    type FirstResolver = (v: { condition: { op: string; value: string } }) => void;
    let resolveFirst: FirstResolver = () => {};
    let parseCalls = 0;
    mswServer.use(
      DEFAULT_FIELDS_HANDLER,
      http.post("/api/v1/condition/parse", async ({ request }) => {
        parseCalls += 1;
        const body = (await request.json()) as { query: string };
        if (body.query === "ab") {
          // Stall: caller resolves manually below.
          return new Promise<HttpResponse>((resolve) => {
            resolveFirst = (v) => resolve(HttpResponse.json(v));
          });
        }
        return HttpResponse.json({
          condition: { op: "SEARCH", value: body.query },
        });
      }),
    );

    const { onChange, getInput } = setup();
    await user.type(getInput(), "ab");

    // Wait for the debounce to schedule the first request.
    await waitFor(() => expect(parseCalls).toBe(1), { timeout: 1500 });

    // Type more before the stalled request resolves. The cleanup flag
    // for the "ab" effect must flip cancelled=true so the late resolve
    // can't write back through onChange.
    await user.type(getInput(), "c");

    // Now release the stale "ab" response.
    resolveFirst({ condition: { op: "SEARCH", value: "ab" } });

    // Wait for the second parse to fire and resolve normally.
    await waitFor(() => expect(parseCalls).toBeGreaterThanOrEqual(2), { timeout: 1500 });
    await waitFor(
      () => {
        const calls = onChange.mock.calls.map((c) => c[0]);
        // Expectations:
        //  - SOME onChange call (the abc parse) carried condition.value === "abc".
        //  - NO onChange call carried { text: "ab", condition: ... } that
        //    would have rolled the input back to "ab".
        const abcCall = calls.find((c) => c.text === "abc" && c.condition !== null);
        expect(abcCall).toBeTruthy();
        const stalewrite = calls.find(
          (c) =>
            c.text === "ab" &&
            c.condition !== null &&
            (c.condition as { value?: unknown }).value === "ab",
        );
        expect(stalewrite).toBeUndefined();
      },
      { timeout: 2000 },
    );
  });
});
