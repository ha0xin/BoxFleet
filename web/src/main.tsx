import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router-dom";

import App from "./App";
import { adminBasename } from "./navigation";
import { initializeColorMode } from "./components/color-mode";
import "@fontsource-variable/inter";
import "./styles/globals.css";

initializeColorMode();

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      // Returning to the tab refreshes whatever is stale; staleTime above keeps
      // rapid tab switches from re-firing every query.
      refetchOnWindowFocus: true,
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
