export type ColorMode = "light" | "dark";

const colorModeStorageKey = "boxfleet.colorMode";

function storedColorMode(): ColorMode | null {
  const mode = localStorage.getItem(colorModeStorageKey);
  return mode === "light" || mode === "dark" ? mode : null;
}

function setColorMode(mode: ColorMode): void {
  document.documentElement.dataset.mode = mode;
}

export function initializeColorMode(): () => void {
  const colorSchemeQuery = window.matchMedia("(prefers-color-scheme: dark)");
  const syncColorMode = () => {
    setColorMode(storedColorMode() ?? (colorSchemeQuery.matches ? "dark" : "light"));
  };

  syncColorMode();
  colorSchemeQuery.addEventListener("change", syncColorMode);
  return () => colorSchemeQuery.removeEventListener("change", syncColorMode);
}

export function toggleColorMode(): ColorMode {
  const mode = document.documentElement.dataset.mode === "dark" ? "light" : "dark";
  localStorage.setItem(colorModeStorageKey, mode);
  setColorMode(mode);
  return mode;
}
