import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { toastStore } from "@/shared/ui/toast/useToast";
import { mswServer } from "@/tests/msw/server";
import { BulkTagDialog } from "./BulkTagDialog";

function setup(props: { q?: string; recordCount?: number; open?: boolean } = {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <BulkTagDialog
          open={props.open ?? true}
          onOpenChange={() => {}}
          q={props.q}
          recordCount={props.recordCount ?? 5}
        />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("BulkTagDialog", () => {
  afterEach(() => {
    toastStore.clear();
  });

  it("renders the set / tag / untag sections", async () => {
    setup({ q: "abc" });
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());
    expect(screen.getByRole("group", { name: /set attributes/i })).toBeInTheDocument();
    expect(screen.getByRole("group", { name: /add tags/i })).toBeInTheDocument();
    expect(screen.getByRole("group", { name: /remove tags/i })).toBeInTheDocument();
  });

  it("submit calls bulk_update with correct set and tag", async () => {
    const calls: Array<{ url: string; body: unknown }> = [];
    mswServer.use(
      http.post("/api/v1/record/bulk_update", async ({ request }) => {
        calls.push({ url: request.url, body: await request.json() });
        return HttpResponse.json({ matched: 5, set: 5, tagged: 5, untagged: 0 });
      }),
    );
    const user = userEvent.setup();
    setup({ q: "abc" });
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());

    // Fill key=environment, value=prod in the first kv pair
    const keyInputs = screen.getAllByRole("textbox", { name: /attribute key/i });
    const valueInputs = screen.getAllByRole("textbox", { name: /attribute value/i });
    await user.type(keyInputs[0]!, "environment");
    await user.type(valueInputs[0]!, "prod");

    // Type a tag in the "Add tags" chip input
    const tagInputs = screen.getByRole("group", { name: /add tags/i });
    const tagInput = tagInputs.querySelector("input")!;
    await user.type(tagInput, "maint");
    await user.keyboard("{Enter}");

    // Submit
    await user.click(screen.getByRole("button", { name: /^apply$/i }));
    await waitFor(() => expect(calls).toHaveLength(1));

    const url = new URL(calls[0]!.url);
    expect(url.searchParams.get("q")).toBe("abc");
    expect(calls[0]!.body).toMatchObject({
      set: { environment: "prod" },
      tag: ["maint"],
    });
  });

  it("empty body is rejected — Submit does not call bulk_update", async () => {
    const calls: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/record/bulk_update", async ({ request }) => {
        calls.push(await request.json());
        return HttpResponse.json({ matched: 0, set: 0, tagged: 0, untagged: 0 });
      }),
    );
    const user = userEvent.setup();
    setup({ q: "abc" });
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());

    // Submit without filling anything
    await user.click(screen.getByRole("button", { name: /^apply$/i }));
    // Give any async ops a moment to settle
    await waitFor(() => {
      const toasts = toastStore.getSnapshot();
      expect(toasts.some((t) => /fill in at least/i.test(t.description))).toBe(true);
    });
    expect(calls).toHaveLength(0);
  });

  it("duplicate tag chips are deduplicated", async () => {
    const calls: Array<{ body: unknown }> = [];
    mswServer.use(
      http.post("/api/v1/record/bulk_update", async ({ request }) => {
        calls.push({ body: await request.json() });
        return HttpResponse.json({ matched: 1, set: 0, tagged: 1, untagged: 0 });
      }),
    );
    const user = userEvent.setup();
    setup({ q: "abc" });
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());

    const tagGroup = screen.getByRole("group", { name: /add tags/i });
    const tagInput = tagGroup.querySelector("input")!;

    // Type "maint" twice
    await user.type(tagInput, "maint");
    await user.keyboard("{Enter}");
    await user.type(tagInput, "maint");
    await user.keyboard("{Enter}");

    await user.click(screen.getByRole("button", { name: /^apply$/i }));
    await waitFor(() => expect(calls).toHaveLength(1));

    const body = calls[0]!.body as { tag?: string[] };
    expect(body.tag).toEqual(["maint"]);
  });

  it("tag count surfaces in success toast", async () => {
    mswServer.use(
      http.post("/api/v1/record/bulk_update", () => {
        return HttpResponse.json({ matched: 5, set: 2, tagged: 3, untagged: 0 });
      }),
    );
    const user = userEvent.setup();
    setup({ q: "abc" });
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());

    const tagGroup = screen.getByRole("group", { name: /add tags/i });
    const tagInput = tagGroup.querySelector("input")!;
    await user.type(tagInput, "maint");
    await user.keyboard("{Enter}");

    await user.click(screen.getByRole("button", { name: /^apply$/i }));
    await waitFor(() => {
      const toasts = toastStore.getSnapshot();
      expect(toasts.some((t) => /5 matched/i.test(t.description))).toBe(true);
      expect(toasts.some((t) => /3 tagged/i.test(t.description))).toBe(true);
    });
  });
});
