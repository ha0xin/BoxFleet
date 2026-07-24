// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import type { AdminRequest } from "./api";
import { useAdminMutation } from "./use-admin-mutation";

describe("useAdminMutation", () => {
  it("keeps the mutation pending and runs onSuccess only after invalidation finishes", async () => {
    const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
    let finishInvalidation!: () => void;
    const invalidation = new Promise<void>((resolve) => { finishInvalidation = resolve; });
    const order: string[] = [];
    vi.spyOn(queryClient, "invalidateQueries").mockImplementation(async () => {
      order.push("invalidate:start");
      await invalidation;
      order.push("invalidate:end");
    });
    const request = vi.fn(async () => ({ id: "updated" })) as unknown as AdminRequest;

    const { result } = renderHook(
      () => useAdminMutation<string, { id: string }>(
        request,
        async () => {
          order.push("mutation");
          return { id: "updated" };
        },
        { onSuccess: () => { order.push("onSuccess"); } }
      ),
      { wrapper: ({ children }: { children: ReactNode }) => <QueryClientProvider client={queryClient}>{children}</QueryClientProvider> }
    );

    let mutationPromise!: Promise<{ id: string }>;
    act(() => {
      mutationPromise = result.current.mutateAsync("payload");
    });
    await waitFor(() => expect(result.current.isPending).toBe(true));
    expect(result.current.isPending).toBe(true);
    expect(order).toEqual(["mutation", "invalidate:start"]);

    finishInvalidation();
    await act(async () => { await mutationPromise; });
    expect(order).toEqual(["mutation", "invalidate:start", "invalidate:end", "onSuccess"]);
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
  });
});
