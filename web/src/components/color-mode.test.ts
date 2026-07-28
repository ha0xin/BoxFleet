// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { initializeColorMode, toggleColorMode } from "./color-mode";

const listeners = new Set<() => void>();
const colorSchemeQuery = {
  matches: false,
  addEventListener: vi.fn((_event: string, listener: () => void) => listeners.add(listener)),
  removeEventListener: vi.fn((_event: string, listener: () => void) => listeners.delete(listener))
};

beforeEach(() => {
  localStorage.clear();
  delete document.documentElement.dataset.mode;
  colorSchemeQuery.matches = false;
  listeners.clear();
  vi.stubGlobal("matchMedia", vi.fn(() => colorSchemeQuery));
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("color mode", () => {
  it("follows the system preference until the user chooses a mode", () => {
    colorSchemeQuery.matches = true;
    const dispose = initializeColorMode();

    expect(document.documentElement.dataset.mode).toBe("dark");

    colorSchemeQuery.matches = false;
    for (const listener of listeners) listener();
    expect(document.documentElement.dataset.mode).toBe("light");

    dispose();
    expect(colorSchemeQuery.removeEventListener).toHaveBeenCalled();
  });

  it("restores a saved mode instead of the system preference", () => {
    colorSchemeQuery.matches = true;
    localStorage.setItem("boxfleet.colorMode", "light");

    initializeColorMode();

    expect(document.documentElement.dataset.mode).toBe("light");
  });

  it("toggles and saves the selected mode", () => {
    document.documentElement.dataset.mode = "light";

    expect(toggleColorMode()).toBe("dark");
    expect(document.documentElement.dataset.mode).toBe("dark");
    expect(localStorage.getItem("boxfleet.colorMode")).toBe("dark");

    expect(toggleColorMode()).toBe("light");
    expect(document.documentElement.dataset.mode).toBe("light");
    expect(localStorage.getItem("boxfleet.colorMode")).toBe("light");
  });
});
