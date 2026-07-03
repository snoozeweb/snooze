import { render, screen } from "@testing-library/react";
import { describe, it, expect, beforeEach } from "vitest";
import { http, HttpResponse } from "msw";
import { QueryClientProvider, QueryClient } from "@tanstack/react-query";
import { mswServer } from "@/tests/msw/server";
import { ApiKeysSection } from "./ApiKeysSection";

// Epochs are computed relative to real Date.now() so no fake timers are
// needed — fake timers break MSW / React Query's async plumbing.
const NOW_SEC = Math.floor(Date.now() / 1000);
// A key last used 40 days ago (> STALE_DAYS=30) → stale.
const STALE_EPOCH = NOW_SEC - 40 * 86_400;
// A key last used 10 days ago → not stale.
const FRESH_EPOCH = NOW_SEC - 10 * 86_400;

function renderSection() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ApiKeysSection />
    </QueryClientProvider>,
  );
}

describe("ApiKeysSection — stale hint", () => {
  beforeEach(() => {
    mswServer.use(
      http.get("/api/v1/user/me/apikeys", () =>
        HttpResponse.json({
          data: [
            {
              uid: "k1",
              owner: "alice",
              name: "stale-bot",
              key_prefix: "snz_abc",
              last_used_at: STALE_EPOCH,
              use_count: 3,
            },
            {
              uid: "k2",
              owner: "alice",
              name: "fresh-bot",
              key_prefix: "snz_def",
              last_used_at: FRESH_EPOCH,
              use_count: 7,
            },
            {
              uid: "k3",
              owner: "alice",
              name: "virgin-bot",
              key_prefix: "snz_ghi",
              /* no last_used_at */
            },
          ],
        }),
      ),
    );
  });

  it("shows a 'Stale' badge for keys unused for > 30 days", async () => {
    renderSection();
    expect(await screen.findByText("stale-bot")).toBeInTheDocument();
    const staleItem = screen.getByText("stale-bot").closest("li")!;
    // Either there's a [data-stale] element, or the text "Stale" is in the item.
    const hasStale =
      staleItem.querySelector("[data-stale]") !== null ||
      (staleItem.textContent ?? "").includes("Stale");
    expect(hasStale).toBe(true);
  });

  it("shows a 'Stale' badge for keys never used", async () => {
    renderSection();
    expect(await screen.findByText("virgin-bot")).toBeInTheDocument();
    const virginItem = screen.getByText("virgin-bot").closest("li")!;
    expect(virginItem.textContent).toMatch(/Stale/);
  });

  it("does NOT show a 'Stale' badge for recently-used keys", async () => {
    renderSection();
    expect(await screen.findByText("fresh-bot")).toBeInTheDocument();
    const freshItem = screen.getByText("fresh-bot").closest("li")!;
    expect(freshItem.textContent).not.toMatch(/Stale/);
  });

  it("shows 'last used ... ago' for keys with last_used_at", async () => {
    renderSection();
    expect(await screen.findByText("fresh-bot")).toBeInTheDocument();
    // The subtitle for the fresh key should mention relative age.
    const freshItem = screen.getByText("fresh-bot").closest("li")!;
    expect(freshItem.textContent).toMatch(/last used/i);
    expect(freshItem.textContent).toMatch(/ago/);
  });

  it("shows 'last used never' when last_used_at is absent", async () => {
    renderSection();
    expect(await screen.findByText("virgin-bot")).toBeInTheDocument();
    const virginItem = screen.getByText("virgin-bot").closest("li")!;
    expect(virginItem.textContent).toMatch(/last used never/i);
  });

  it("shows use_count when non-zero", async () => {
    renderSection();
    expect(await screen.findByText("stale-bot")).toBeInTheDocument();
    const staleItem = screen.getByText("stale-bot").closest("li")!;
    expect(staleItem.textContent).toMatch(/3 uses/);
  });
});
