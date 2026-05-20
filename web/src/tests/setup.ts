import "@testing-library/jest-dom/vitest";
import { afterAll, afterEach, beforeAll } from "vitest";
import { cleanup } from "@testing-library/react";
import { mswServer } from "./msw/server";

// Radix UI uses pointer capture and scrollIntoView APIs that jsdom doesn't
// implement. Polyfill them so tests can interact with Radix components.
if (typeof window !== "undefined") {
  if (!window.HTMLElement.prototype.hasPointerCapture) {
    window.HTMLElement.prototype.hasPointerCapture = () => false;
  }
  if (!window.HTMLElement.prototype.setPointerCapture) {
    window.HTMLElement.prototype.setPointerCapture = () => undefined;
  }
  if (!window.HTMLElement.prototype.releasePointerCapture) {
    window.HTMLElement.prototype.releasePointerCapture = () => undefined;
  }
  if (!window.HTMLElement.prototype.scrollIntoView) {
    window.HTMLElement.prototype.scrollIntoView = () => undefined;
  }
}

// jsdom 25 ships its own AbortController/AbortSignal which Node's undici
// fetch (and MSW's fetch interceptor, which wraps it) rejects via
// `instanceof AbortSignal`. react-query passes the queryFn's signal to
// fetch, so every test that hits the API client used to die with
//   `TypeError: RequestInit: Expected signal ("AbortSignal {}") to be an
//    instance of AbortSignal.`
// Wrap whatever fetch is currently installed, probing the signal with
// `new Request(..., { signal })`; if it isn't accepted, drop `signal`
// transparently. The wrap must be re-applied AFTER `mswServer.listen()`
// because MSW replaces `globalThis.fetch` with its own interceptor at that
// point. Production code is unaffected — this only runs in tests.
function installFetchSignalShim() {
  const current = globalThis.fetch;
  if (typeof current !== "function") return;
  // Idempotent: tag the shim so we don't wrap our own wrapper if reinstalled.
  const tag = "__signalShim";
  if ((current as unknown as { [k: string]: unknown })[tag] === true) return;
  const real = current.bind(globalThis);
  const incompatibleProtos = new WeakSet<object>();
  const shim = function fetchSignalShim(
    input: RequestInfo | URL,
    init?: RequestInit,
  ): Promise<Response> {
    const sig = init?.signal;
    if (sig) {
      const proto = Object.getPrototypeOf(sig) as object | null;
      let incompatible = proto !== null && incompatibleProtos.has(proto);
      if (!incompatible) {
        try {
          new Request("http://probe.local/", { signal: sig });
        } catch {
          incompatible = true;
          if (proto !== null) incompatibleProtos.add(proto);
        }
      }
      if (incompatible) {
        const { signal: _drop, ...rest } = init;
        void _drop;
        return real(input, rest);
      }
    }
    return real(input, init);
  };
  Object.defineProperty(shim, tag, { value: true });
  Object.defineProperty(globalThis, "fetch", {
    value: shim,
    writable: true,
    configurable: true,
  });
  if (typeof window !== "undefined") {
    Object.defineProperty(window, "fetch", {
      value: shim,
      writable: true,
      configurable: true,
    });
  }
}

beforeAll(() => {
  mswServer.listen({ onUnhandledRequest: "warn" });
  // MSW's setupServer just replaced globalThis.fetch with its own
  // interceptor. Wrap it again so the signal-probe path runs first.
  installFetchSignalShim();
});

afterEach(() => {
  cleanup();
  mswServer.resetHandlers();
});

afterAll(() => {
  mswServer.close();
});
