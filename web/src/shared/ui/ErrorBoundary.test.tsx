import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ErrorBoundary } from "./ErrorBoundary";

function Boom({ throws }: { throws: boolean }) {
  if (throws) throw new Error("Failed to fetch dynamically imported module");
  return <p>the editor</p>;
}

describe("ErrorBoundary", () => {
  // React logs every caught error to console.error; silence it so a passing
  // run isn't a wall of red.
  let spy: ReturnType<typeof vi.spyOn>;
  beforeEach(() => {
    spy = vi.spyOn(console, "error").mockImplementation(() => undefined);
  });
  afterEach(() => spy.mockRestore());

  it("renders its children while nothing throws", () => {
    render(
      <ErrorBoundary>
        <Boom throws={false} />
      </ErrorBoundary>,
    );
    expect(screen.getByText("the editor")).toBeInTheDocument();
  });

  it("catches a render failure and offers a reload instead of taking the route down", async () => {
    const user = userEvent.setup();
    const reload = vi.fn();
    vi.stubGlobal("location", { ...window.location, reload });
    render(
      <ErrorBoundary summary="The analysis editor could not be loaded.">
        <Boom throws />
      </ErrorBoundary>,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("The analysis editor could not be loaded.");
    await user.click(screen.getByRole("button", { name: "Reload" }));
    expect(reload).toHaveBeenCalledTimes(1);
    vi.unstubAllGlobals();
  });

  it("clears the caught error when resetKey changes", () => {
    const { rerender } = render(
      <ErrorBoundary resetKey="a">
        <Boom throws />
      </ErrorBoundary>,
    );
    expect(screen.getByRole("alert")).toBeInTheDocument();

    // The boundary wraps a subject (a row, a uid); pointing it at another one
    // is a fresh attempt, not the same failure.
    rerender(
      <ErrorBoundary resetKey="b">
        <Boom throws={false} />
      </ErrorBoundary>,
    );
    expect(screen.getByText("the editor")).toBeInTheDocument();
  });

  it("hands the error and a reset to a fallback render prop", async () => {
    const user = userEvent.setup();
    let broken = true;
    function Flaky() {
      if (broken) throw new Error("chunk 404");
      return <p>the editor</p>;
    }
    render(
      <ErrorBoundary
        fallback={(error, reset) => (
          <div>
            <p>{error.message}</p>
            <button type="button" onClick={reset}>
              Try again
            </button>
          </div>
        )}
      >
        <Flaky />
      </ErrorBoundary>,
    );
    expect(screen.getByText("chunk 404")).toBeInTheDocument();
    // The deploy settled / the network came back: a retry can now succeed.
    broken = false;
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(screen.getByText("the editor")).toBeInTheDocument();
  });
});
