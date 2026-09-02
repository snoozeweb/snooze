import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LiveAnnouncerProvider, useAnnounce, type AnnounceFn } from "./LiveAnnouncer";

/** Captured out of a render so tests can call announce() directly instead of
 *  driving a fake button through user-event (which does not mix with the fake
 *  timers these assertions need). */
let announce: AnnounceFn = () => undefined;

function Probe() {
  announce = useAnnounce();
  return null;
}

function renderAnnouncer() {
  render(
    <LiveAnnouncerProvider>
      <Probe />
    </LiveAnnouncerProvider>,
  );
}

const polite = () => screen.getByTestId("live-polite");
const assertive = () => screen.getByTestId("live-assertive");

/** Advance past the coalesce window and the clear-then-set repaint gap. */
function flush() {
  act(() => {
    vi.advanceTimersByTime(400);
  });
}

describe("LiveAnnouncer", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders one polite and one assertive region, both atomic", () => {
    renderAnnouncer();
    expect(polite()).toHaveAttribute("aria-live", "polite");
    expect(polite()).toHaveAttribute("aria-atomic", "true");
    expect(polite()).toHaveAttribute("role", "status");
    expect(assertive()).toHaveAttribute("aria-live", "assertive");
    expect(assertive()).toHaveAttribute("aria-atomic", "true");
    expect(assertive()).toHaveAttribute("role", "alert");
    expect(polite()).toHaveTextContent("");
    expect(assertive()).toHaveTextContent("");
  });

  it("announce() writes to the polite region", () => {
    renderAnnouncer();
    act(() => announce("Alerts refreshed. 4 alerts, 1 new."));
    flush();
    expect(polite()).toHaveTextContent("Alerts refreshed. 4 alerts, 1 new.");
    expect(assertive()).toHaveTextContent("");
  });

  it("assertive announcements go to the other region", () => {
    renderAnnouncer();
    act(() => announce("Cluster status: 1 issue detected.", { assertive: true }));
    flush();
    expect(assertive()).toHaveTextContent("Cluster status: 1 issue detected.");
    expect(polite()).toHaveTextContent("");
  });

  it("re-announces an identical message by blanking the region first", () => {
    renderAnnouncer();
    act(() => announce("same"));
    flush();
    expect(polite()).toHaveTextContent("same");

    act(() => announce("same"));
    // Coalesce window elapsed: the region is empty, which is the DOM change a
    // screen reader needs before it will read the identical text again.
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(polite().textContent).toBe("");
    act(() => {
      vi.advanceTimersByTime(50);
    });
    expect(polite()).toHaveTextContent("same");
  });

  it("keeps only the last of a burst", () => {
    renderAnnouncer();
    act(() => {
      announce("first");
      announce("second");
      announce("third");
    });
    flush();
    expect(polite()).toHaveTextContent("third");
  });

  it("clears the region so stale text is not re-read on focus", () => {
    renderAnnouncer();
    act(() => announce("Alerts refreshed. 1 alert, 1 fewer."));
    flush();
    expect(polite()).toHaveTextContent("Alerts refreshed. 1 alert, 1 fewer.");
    act(() => {
      vi.advanceTimersByTime(5000);
    });
    expect(polite().textContent).toBe("");
  });

  it("is a no-op outside the provider instead of throwing", () => {
    render(<Probe />);
    expect(() => announce("nobody is listening")).not.toThrow();
  });
});
