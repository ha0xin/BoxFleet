import { createContext, useCallback, useContext, useMemo } from "react";
import type { ReactNode } from "react";

import { adminBasename } from "@/navigation";

export type AdminRequest = <T>(path: string, init?: RequestInit) => Promise<T>;

export class AdminApiError extends Error {
  readonly status: number;
  readonly detail: string;

  constructor(status: number, detail: string) {
    super(detail || `Request failed with status ${status}`);
    this.name = "AdminApiError";
    this.status = status;
    this.detail = detail;
  }
}

function adminRequestPath(path: string): string {
  if (!path.startsWith("/api/admin")) return path;
  const basename = adminBasename();
  return basename === "/admin" ? path : `${basename.slice(0, -"/admin".length)}${path}`;
}

async function responseError(response: Response): Promise<AdminApiError> {
  const body = await response.text();
  const contentType = (response.headers.get("Content-Type") ?? "").toLowerCase();
  const fallback = response.status >= 500
    ? `The server is temporarily unavailable (HTTP ${response.status}). Please try again shortly.`
    : `Request failed (HTTP ${response.status}).`;
  const readableDetail = (value: unknown): string | undefined => {
    if (typeof value !== "string") return undefined;
    const detail = value.trim();
    if (!detail || detail.length > 500 || /<\/?[a-z][^>]*>|<!doctype/i.test(detail)) return undefined;
    return detail;
  };
  if (contentType.includes("application/json")) {
    try {
      const value = JSON.parse(body) as { error?: unknown };
      return new AdminApiError(response.status, readableDetail(value?.error) ?? fallback);
    } catch {
      return new AdminApiError(response.status, fallback);
    }
  }
  return new AdminApiError(response.status, contentType.includes("text/html") ? fallback : readableDetail(body) ?? fallback);
}

export async function readAdminResponse<T>(response: Response): Promise<T> {
  if (!response.ok) throw await responseError(response);
  if (response.status === 204) return undefined as T;
  return (response.headers.get("Content-Type") ?? "").includes("application/json")
    ? ((await response.json()) as T)
    : ((await response.text()) as T);
}

const AdminApiContext = createContext<{ request: AdminRequest } | null>(null);

export function AdminApiProvider({
  token,
  onUnauthorized,
  children
}: {
  token: string;
  onUnauthorized?: () => void;
  children: ReactNode;
}) {
  const request = useCallback<AdminRequest>(async <T,>(path: string, init: RequestInit = {}) => {
    const headers = new Headers(init.headers);
    if (!headers.has("Content-Type") && init.body) headers.set("Content-Type", "application/json");
    if (token.trim()) headers.set("Authorization", `Bearer ${token.trim()}`);
    let response: Response;
    try {
      response = await fetch(adminRequestPath(path), { ...init, headers });
    } catch (error) {
      // Preserve AbortError so navigation can cancel queries normally.
      if (error instanceof TypeError) {
        throw new AdminApiError(0, "Unable to reach the server. Check your connection and try again.");
      }
      throw error;
    }
    try {
      return await readAdminResponse<T>(response);
    } catch (error) {
      if (error instanceof AdminApiError && error.status === 401) onUnauthorized?.();
      throw error;
    }
  }, [onUnauthorized, token]);

  const value = useMemo(() => ({ request }), [request]);
  return <AdminApiContext.Provider value={value}>{children}</AdminApiContext.Provider>;
}

export function useAdminApi() {
  const value = useContext(AdminApiContext);
  if (!value) throw new Error("useAdminApi must be used inside AdminApiProvider");
  return value;
}
