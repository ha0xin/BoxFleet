// @vitest-environment jsdom

import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { getIsDarkMode, useIsDarkMode } from "./use-color-mode";

// Vitest runs without globals, so Testing Library never registers auto-cleanup.
afterEach(() => {
  cleanup();
  delete document.documentElement.dataset.mode;
});

/** MutationObserver callbacks are queued as microtasks; let them drain. */
async function setMode(mode: string) {
  await act(async () => {
    document.documentElement.dataset.mode = mode;
    await Promise.resolve();
  });
}

describe("useIsDarkMode", () => {
  it("reads the mode written by main.tsx", () => {
    expect(getIsDarkMode()).toBe(false);
    document.documentElement.dataset.mode = "dark";
    expect(getIsDarkMode()).toBe(true);
  });

  it("re-renders when data-mode changes after mount", async () => {
    document.documentElement.dataset.mode = "light";
    const { result } = renderHook(() => useIsDarkMode());
    expect(result.current).toBe(false);

    await setMode("dark");
    expect(result.current).toBe(true);

    await setMode("light");
    expect(result.current).toBe(false);
  });

  it("keeps every subscriber in sync and stops observing once all unmount", async () => {
    document.documentElement.dataset.mode = "light";
    const first = renderHook(() => useIsDarkMode());
    const second = renderHook(() => useIsDarkMode());

    await setMode("dark");
    expect(first.result.current).toBe(true);
    expect(second.result.current).toBe(true);

    first.unmount();
    await setMode("light");
    expect(second.result.current).toBe(false);

    second.unmount();
    await setMode("dark");
    expect(getIsDarkMode()).toBe(true);
  });
});
