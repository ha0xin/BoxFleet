// @vitest-environment jsdom

import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AdminApiProvider } from "@/admin/api";
import { MihomoProfilesPage } from "./mihomo-profiles";

// Monaco pulls in `?worker` imports that only Vite's dev/build pipeline can
// resolve, and the editor is not on this page's load path anyway.
vi.mock("@/components/mihomo-code-editor", () => ({
  MihomoCodeEditor: () => null
}));

// The shared header pulls in the sidebar and publish-status contexts.
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

describe("MihomoProfilesPage", () => {
  it("passes the query signal to the polling requests", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } })
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <MemoryRouter>
        <QueryClientProvider client={client}>
          <AdminApiProvider token="">
            <MihomoProfilesPage />
          </AdminApiProvider>
        </QueryClientProvider>
      </MemoryRouter>
    );

    // The configuration list and the rewrite templates both poll.
    await waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThanOrEqual(2));
    for (const call of fetchMock.mock.calls) {
      expect((call[1] as RequestInit | undefined)?.signal).toBeInstanceOf(AbortSignal);
    }
  });
});
