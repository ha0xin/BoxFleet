import { createKumoToastManager } from "@cloudflare/kumo";

/**
 * App-wide toast manager, mounted by the `<Toasty>` viewport in App.tsx.
 * Module-level so non-React code (mutation handlers, query listeners) can
 * dispatch toasts.
 */
export const adminToastManager = createKumoToastManager();

export function toastError(title: string, description?: string) {
  adminToastManager.add({ variant: "error", title, description });
}
