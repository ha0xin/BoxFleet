// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ProxyFormDialog } from "./proxy-dialogs";

// Vitest runs without globals, so Testing Library never registers auto-cleanup.
afterEach(cleanup);

describe("ProxyFormDialog", () => {
  it("passes the query signal to the node lookup", async () => {
    const request = vi.fn().mockResolvedValue([]);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

    render(
      <QueryClientProvider client={client}>
        <ProxyFormDialog request={request} state={{ mode: "create" }} onClose={() => {}} />
      </QueryClientProvider>
    );

    await waitFor(() => expect(request).toHaveBeenCalledWith("/api/admin/nodes", expect.anything()));
    const init = request.mock.calls[0][1] as RequestInit;
    expect(init.signal).toBeInstanceOf(AbortSignal);
  });
});
