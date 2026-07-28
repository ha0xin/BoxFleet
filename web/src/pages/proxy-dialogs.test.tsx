// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
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

  it("edits the current direct Path publication setting", async () => {
    const request = vi.fn().mockResolvedValue({});
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

    render(
      <QueryClientProvider client={client}>
        <ProxyFormDialog
          request={request}
          state={{
            mode: "edit",
            proxy: {
              id: "prx_test",
              node_name: "edge-1",
              name: "private",
              protocol: "vless_reality",
              listen: "::",
              listen_port: 443,
              transport: "tcp",
              enabled: true,
              traffic_multiplier: 1,
              direct_publish: false,
              short_id: "",
              settings_json: "{}",
              inbound_rules_json: "",
              outbound_rules_json: "",
              route_rules_json: "",
              created_at: "2026-07-28T00:00:00Z",
              updated_at: "2026-07-28T00:00:00Z",
              deleted_at: ""
            }
          }}
          onClose={() => {}}
        />
      </QueryClientProvider>
    );

    const directSwitch = screen.getByRole("switch", { name: "Publish direct Paths" });
    expect(directSwitch.getAttribute("aria-checked")).toBe("false");
    fireEvent.click(directSwitch);
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() => expect(request).toHaveBeenCalledTimes(1));
    const init = request.mock.calls[0][1] as RequestInit;
    expect(JSON.parse(String(init.body))).toMatchObject({ direct_publish: true });
  });
});
