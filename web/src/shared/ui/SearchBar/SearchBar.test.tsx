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
});
