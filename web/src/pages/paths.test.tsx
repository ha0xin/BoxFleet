// @vitest-environment jsdom

import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AdminApiProvider } from "@/admin/api";
import { PathsPage } from "./paths";

// The shared header pulls in the sidebar and publish-status contexts, neither of
// which says anything about Paths. Stub it down to the slots this page fills.
vi.mock("@/components/app-page-header", () => ({
  AppPageHeader: ({ title, actions }: { title: string; actions?: ReactNode }) => (
    <header>
      <h1>{title}</h1>
      {actions}
    </header>
  )
}));

// Vitest runs without globals, so Testing Library never registers auto-cleanup.
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" }
  });
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <AdminApiProvider token="">
        <PathsPage />
      </AdminApiProvider>
    </QueryClientProvider>
  );
}

/** The `RequestInit.signal` of the most recent request to `path`. */
function signalFor(fetchMock: ReturnType<typeof vi.fn>, path: string): AbortSignal {
  const call = fetchMock.mock.calls.filter((entry) => String(entry[0]) === path).at(-1);
  if (!call) throw new Error(`no request was made to ${path}`);
  return (call[1] as RequestInit | undefined)?.signal as AbortSignal;
}

describe("PathsPage", () => {
  it("passes the query signal to every request so navigation cancels them", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse([]));
    vi.stubGlobal("fetch", fetchMock);

    renderPage();
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(signalFor(fetchMock, "/api/admin/paths")).toBeInstanceOf(AbortSignal);

    // The editor's proxy and node lookups are separate queries with the same gap.
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    await waitFor(() => expect(signalFor(fetchMock, "/api/admin/nodes")).toBeInstanceOf(AbortSignal));
    expect(signalFor(fetchMock, "/api/admin/proxies?limit=500")).toBeInstanceOf(AbortSignal);
  });

  it("aborts the polling request when the page unmounts", async () => {
    // A request that never settles keeps the query in flight across the unmount.
    const fetchMock = vi.fn().mockReturnValue(new Promise<Response>(() => {}));
    vi.stubGlobal("fetch", fetchMock);

    const view = renderPage();
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const signal = signalFor(fetchMock, "/api/admin/paths");
    expect(signal.aborted).toBe(false);

    view.unmount();
    // React Query only cancels a query whose fn consumed the signal, so this
    // fails the moment the queryFn stops forwarding it.
    await waitFor(() => expect(signal.aborted).toBe(true));
  });
});
