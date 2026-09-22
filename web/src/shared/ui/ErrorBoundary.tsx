import { Component } from "react";
import type { ErrorInfo, ReactNode } from "react";
import { Button } from "./Button";
import { InlineError } from "./InlineError";
import styles from "./ErrorBoundary.module.css";

export type ErrorBoundaryProps = {
  children: ReactNode;
  /**
   * Render the failure yourself. Gets the thrown error and a `reset` that
   * clears the boundary and re-renders the children — worth offering when the
   * failure is plausibly transient (a lazy chunk, a flaky render).
   */
  fallback?: (error: Error, reset: () => void) => ReactNode;
  /**
   * The subject the guarded subtree is about (a row key, a uid). Changing it
   * clears a caught error: the boundary is being pointed at something else, so
   * the previous failure says nothing about the new attempt.
   */
  resetKey?: unknown;
  /** First line of the default fallback. */
  summary?: string;
};

type ErrorBoundaryState = { error: Error | null };

/**
 * A render-error boundary — the only thing in React that can stop one broken
 * subtree from unmounting everything above it.
 *
 * The case it exists for is a **lazy chunk that 404s after a deploy**: the
 * bundle the open tab was built against is gone from the server, `import()`
 * rejects, and the rejection surfaces as a render error. Without a boundary
 * that error escapes to the router, which replaces the whole route — an
 * operator loses the alert list because a tab they were not even looking at
 * failed to fetch. Reloading is the actual fix (it pulls the new bundle), so
 * the default fallback says so and offers the button.
 *
 * Class component on purpose: `getDerivedStateFromError` has no hook
 * equivalent. This is the one class in the SPA.
 */
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  override state: ErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: unknown): ErrorBoundaryState {
    return { error: error instanceof Error ? error : new Error(String(error)) };
  }

  override componentDidCatch(error: Error, info: ErrorInfo): void {
    // The console is where a support engineer looks first; React's own message
    // carries the component stack, which the fallback deliberately does not.
    console.error("ErrorBoundary caught", error, info.componentStack);
  }

  override componentDidUpdate(prev: ErrorBoundaryProps): void {
    if (this.state.error !== null && prev.resetKey !== this.props.resetKey) {
      this.setState({ error: null });
    }
  }

  private readonly reset = (): void => this.setState({ error: null });

  override render(): ReactNode {
    const { error } = this.state;
    if (error === null) return this.props.children;
    if (this.props.fallback) return this.props.fallback(error, this.reset);
    return (
      <div className={styles.fallback}>
        <InlineError
          summary={this.props.summary ?? "This part of the page failed to load."}
          secondary={error.message}
        />
        <Button
          variant="secondary"
          size="sm"
          leadingIcon="refresh"
          onClick={() => location.reload()}
        >
          Reload
        </Button>
      </div>
    );
  }
}
