import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

afterEach(() => {
  cleanup();
});

// Radix UI Select uses pointer capture and scrollIntoView APIs that jsdom
// doesn't implement. Polyfill them so tests can interact with the component.
window.HTMLElement.prototype.hasPointerCapture = () => false;
window.HTMLElement.prototype.setPointerCapture = () => undefined;
window.HTMLElement.prototype.releasePointerCapture = () => undefined;
window.HTMLElement.prototype.scrollIntoView = () => undefined;
