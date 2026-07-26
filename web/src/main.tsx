import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router-dom";

import App from "./App";
import { adminBasename } from "./navigation";
import "@fontsource-variable/inter";
import "./styles/globals.css";

// Kumo resolves light/dark via `data-mode` on the root element; follow the
// system preference and track changes live.
const colorSchemeQuery = window.matchMedia("(prefers-color-scheme: dark)");
function applyColorMode() {
  document.documentElement.dataset.mode = colorSchemeQuery.matches ? "dark" : "light";
}
applyColorMode();
colorSchemeQuery.addEventListener("change", applyColorMode);

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      refetchOnWindowFocus: false,
      retry: false
    }
  }
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter basename={adminBasename()}>
        <App />
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>
);
