// @vitest-environment jsdom

import { renderHook } from "@testing-library/react";
import { createElement } from "react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AdminApiError, AdminApiProvider, readAdminResponse, useAdminApi } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("readAdminResponse", () => {
  it("preserves structured API error details", async () => {
    const response = new Response(JSON.stringify({ error: "name is required" }), {
      status: 422,
      headers: { "Content-Type": "application/json" }
    });

    await expect(readAdminResponse(response)).rejects.toEqual(
      expect.objectContaining<Partial<AdminApiError>>({ status: 422, detail: "name is required" })
    );
  });

  it("supports JSON, text, and empty successful responses", async () => {
    await expect(readAdminResponse(new Response(JSON.stringify({ ok: true }), {
      headers: { "Content-Type": "application/json" }
    }))).resolves.toEqual({ ok: true });
    await expect(readAdminResponse(new Response("yaml"))).resolves.toBe("yaml");
    await expect(readAdminResponse(new Response(null, { status: 204 }))).resolves.toBeUndefined();
  });

  it.each([
    { status: 502, type: "text/html", body: "<!DOCTYPE html><html><body>Cloudflare gateway error</body></html>" },
    { status: 503, type: "text/plain", body: "<html><body>Service unavailable</body></html>" },
    { status: 502, type: "application/json", body: JSON.stringify({ error: "<div>Bad gateway</div>" }) },
    { status: 502, type: "application/json", body: "<!DOCTYPE html><html>Malformed upstream response</html>" },
    { status: 503, type: "text/plain", body: "x".repeat(600) }
  ])("summarizes upstream errors without exposing HTML or oversized bodies ($type, $status)", async ({ status, type, body }) => {
    await expect(readAdminResponse(new Response(body, { status, headers: { "Content-Type": type } }))).rejects.toEqual(
      expect.objectContaining<Partial<AdminApiError>>({ status, detail: `The server is temporarily unavailable (HTTP ${status}). Please try again shortly.` })
    );
  });

  it("preserves concise plain-text validation errors", async () => {
    await expect(readAdminResponse(new Response("invalid proxy port", { status: 422 }))).rejects.toEqual(
      expect.objectContaining<Partial<AdminApiError>>({ status: 422, detail: "invalid proxy port" })
    );
  });

  it("explains network failures and preserves cancellation", async () => {
    const fetchMock = vi.fn().mockRejectedValueOnce(new TypeError("Failed to fetch"));
    vi.stubGlobal("fetch", fetchMock);
    const { result } = renderHook(() => useAdminApi(), {
      wrapper: ({ children }: { children: ReactNode }) => createElement(AdminApiProvider, { token: "", children })
    });
    await expect(result.current.request("/api/admin/nodes")).rejects.toEqual(
      expect.objectContaining<Partial<AdminApiError>>({ status: 0, detail: "Unable to reach the server. Check your connection and try again." })
    );
    const aborted = new DOMException("Request cancelled", "AbortError");
    fetchMock.mockRejectedValueOnce(aborted);
    await expect(result.current.request("/api/admin/nodes")).rejects.toBe(aborted);
  });

  it("reports rejected admin credentials to the application shell", async () => {
    const onUnauthorized = vi.fn();
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: "unauthorized" }), {
      status: 401,
      headers: { "Content-Type": "application/json" }
    })));
    const { result } = renderHook(() => useAdminApi(), {
      wrapper: ({ children }: { children: ReactNode }) =>
        createElement(AdminApiProvider, { token: "expired", onUnauthorized, children })
    });

    await expect(result.current.request("/api/admin/overview")).rejects.toEqual(
      expect.objectContaining<Partial<AdminApiError>>({ status: 401 })
    );
    expect(onUnauthorized).toHaveBeenCalledOnce();
  });
});
