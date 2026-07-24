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
