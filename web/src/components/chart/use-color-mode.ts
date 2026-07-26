import { useSyncExternalStore } from "react";

const listeners = new Set<() => void>();
let observer: MutationObserver | null = null;

/**
 * Current colour mode. `src/main.tsx` is the single writer of `data-mode` on the
 * root element, so reading the attribute is the whole story.
 */
export function getIsDarkMode(): boolean {
  return typeof document !== "undefined" && document.documentElement.dataset.mode === "dark";
}

function subscribe(onStoreChange: () => void): () => void {
  listeners.add(onStoreChange);
  if (!observer && typeof MutationObserver !== "undefined") {
    observer = new MutationObserver(() => {
      for (const listener of listeners) listener();
    });
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-mode"] });
  }
  return () => {
    listeners.delete(onStoreChange);
    if (listeners.size === 0) {
      observer?.disconnect();
      observer = null;
    }
  };
}

/**
 * Bridges Kumo's CSS-level theming to canvas charts. Kumo resolves light/dark
 * with `light-dark()`, which an ECharts canvas cannot read, so palette lookups
 * need the mode as a value. Observing `data-mode` rather than adding a second
 * `matchMedia` listener keeps this correct if a manual theme toggle is added.
 */
export function useIsDarkMode(): boolean {
  return useSyncExternalStore(subscribe, getIsDarkMode, () => false);
}
