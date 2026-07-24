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
  if ((response.headers.get("Content-Type") ?? "").includes("application/json")) {
    try {
      const value = JSON.parse(body) as { error?: unknown };
      if (typeof value.error === "string") return new AdminApiError(response.status, value.error);
    } catch {
      // Fall through to the raw response body for malformed error responses.
    }
  }
  return new AdminApiError(response.status, body.trim());
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
    const response = await fetch(adminRequestPath(path), { ...init, headers });
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
