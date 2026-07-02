import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { SavedSearch } from "./savedSearchApi";

// Mocked hook state, reset per test.
const listData: { data: SavedSearch[] } = { data: [] };
const createMutate = vi.fn();
const removeMutate = vi.fn();

vi.mock("./savedSearchApi", () => ({
  SavedSearches: {
    useList: () => ({ data: listData, isPending: false }),
    useCreate: () => ({ mutateAsync: createMutate, isPending: false }),
    useRemove: () => ({ mutateAsync: removeMutate, isPending: false }),
  },
}));

// Import after the mock is registered.
import { SavedSearches } from "./SavedSearches";

beforeEach(() => {
  listData.data = [];
  createMutate.mockReset();
  createMutate.mockResolvedValue({ uid: "new", name: "x", query: "q" });
  removeMutate.mockReset();
  removeMutate.mockResolvedValue(undefined);
});

/** Expand the collapsible panel so its body is rendered. */
async function open(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: /saved searches/i }));
}

describe("SavedSearches", () => {
  it("renders an empty state when there are no saved searches", async () => {
    const user = userEvent.setup();
    render(<SavedSearches currentQuery="" onApply={() => undefined} />);
    await open(user);
    expect(screen.getByText(/no saved searches/i)).toBeInTheDocument();
  });

  it("applies a saved search's query on click", async () => {
    listData.data = [{ uid: "s1", name: "prod criticals", query: "severity = critical" }];
    const onApply = vi.fn();
    const user = userEvent.setup();
    render(<SavedSearches currentQuery="" onApply={onApply} />);
    await open(user);
    await user.click(screen.getByRole("button", { name: /apply prod criticals/i }));
    expect(onApply).toHaveBeenCalledWith("severity = critical");
  });

  it("notes that saving captures only the query, not the tab or environment", async () => {
    const user = userEvent.setup();
    render(<SavedSearches currentQuery={'host = "srv"'} onApply={() => undefined} />);
    await open(user);
    expect(screen.getByText(/not the active tab or environment/i)).toBeInTheDocument();
  });

  it("saves the current query when given a name", async () => {
    const user = userEvent.setup();
    render(<SavedSearches currentQuery="host = web" onApply={() => undefined} />);
    await open(user);
    await user.type(screen.getByLabelText(/name/i), "web hosts");
    await user.click(screen.getByRole("button", { name: /^save$/i }));
    await waitFor(() => expect(createMutate).toHaveBeenCalledTimes(1));
    expect(createMutate).toHaveBeenCalledWith({ name: "web hosts", query: "host = web" });
  });

  it("rejects saving with an empty name", async () => {
    const user = userEvent.setup();
    render(<SavedSearches currentQuery="host = web" onApply={() => undefined} />);
    await open(user);
    await user.click(screen.getByRole("button", { name: /^save$/i }));
    expect(createMutate).not.toHaveBeenCalled();
  });

  it("deletes a saved search", async () => {
    listData.data = [{ uid: "s1", name: "prod criticals", query: "severity = critical" }];
    const user = userEvent.setup();
    render(<SavedSearches currentQuery="" onApply={() => undefined} />);
    await open(user);
    await user.click(screen.getByRole("button", { name: /delete prod criticals/i }));
    await waitFor(() => expect(removeMutate).toHaveBeenCalledWith("s1"));
  });
});
