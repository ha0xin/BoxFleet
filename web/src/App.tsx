import { Check, LogOut, RefreshCw, Settings2 } from "lucide-react";
import { lazy, Suspense, useMemo, useState } from "react";
import { useIsFetching, useQuery, useQueryClient } from "@tanstack/react-query";
import { Navigate, NavLink, Route, Routes, useLocation } from "react-router-dom";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { LoadingDots } from "@/components/ui/loading-dots";
import { Note } from "@/components/ui/note";

import { adminBasename, pages } from "./navigation";
import type { Overview, SystemLogsResponse } from "./types";

const NetworkEventsPage = lazy(() => import("./pages").then((module) => ({ default: module.NetworkEventsPage })));
const NodesPage = lazy(() => import("./pages").then((module) => ({ default: module.NodesPage })));
const OverviewPage = lazy(() => import("./pages").then((module) => ({ default: module.OverviewPage })));
const ProxiesPage = lazy(() => import("./pages").then((module) => ({ default: module.ProxiesPage })));
const SystemLogsPage = lazy(() => import("./pages").then((module) => ({ default: module.SystemLogsPage })));
const TrafficPage = lazy(() => import("./pages").then((module) => ({ default: module.TrafficPage })));
const UsersPage = lazy(() => import("./pages").then((module) => ({ default: module.UsersPage })));

type Requester = <T>(path: string, init?: RequestInit) => Promise<T>;

function adminRequestPath(path: string): string {
  if (!path.startsWith("/api/admin")) {
    return path;
  }
  const basename = adminBasename();
  if (basename === "/admin") {
    return path;
  }
  return `${basename.slice(0, -"/admin".length)}${path}`;
}

function App() {
  const queryClient = useQueryClient();
  const location = useLocation();
  const [activeToken, setActiveToken] = useState(() => localStorage.getItem("boxfleet.adminToken") ?? "");
  const [tokenInput, setTokenInput] = useState(activeToken);
  const [authVersion, setAuthVersion] = useState(0);
  const adminFetching = useIsFetching({ queryKey: ["admin"] }) > 0;

  async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers = new Headers(init.headers);
    if (!headers.has("Content-Type") && init.body) {
      headers.set("Content-Type", "application/json");
    }
    if (activeToken.trim()) {
      headers.set("Authorization", `Bearer ${activeToken.trim()}`);
    }
    const response = await fetch(adminRequestPath(path), { ...init, headers });
    if (!response.ok) {
      const body = await response.text();
      if (response.status === 401) {
        throw new Error("Admin token is missing or invalid.");
      }
      throw new Error(`${response.status} ${body}`);
    }
    return (await response.json()) as T;
  }

  async function refresh() {
    await queryClient.invalidateQueries({ queryKey: ["admin"] });
  }

  function applyToken() {
    const next = tokenInput.trim();
    setActiveToken(next);
    if (next) {
      localStorage.setItem("boxfleet.adminToken", next);
    } else {
      localStorage.removeItem("boxfleet.adminToken");
    }
    queryClient.clear();
    setAuthVersion((value) => value + 1);
  }

  function logout() {
    setTokenInput("");
    setActiveToken("");
    localStorage.removeItem("boxfleet.adminToken");
    queryClient.clear();
    setAuthVersion((value) => value + 1);
  }

  const overviewQuery = useQuery({
    queryKey: ["admin", "overview", authVersion],
    queryFn: () => request<Overview>("/api/admin/overview")
  });

  const overview = overviewQuery.data ?? null;
  const loading = overviewQuery.isLoading;
  const error = overviewQuery.error;

  const trafficRows = overview?.traffic ?? [];
  const totalTraffic = useMemo(() => trafficRows.reduce((sum, row) => sum + row.billable_bytes, 0), [trafficRows]);
  const activeNodes = overview?.nodes.filter((node) => node.status === "active").length ?? 0;
  const activeUsers = overview?.users.filter((user) => user.status === "active").length ?? 0;
  const currentPage = pages.find((item) => item.path === location.pathname) ?? pages[0];

  return (
    <div className="grid min-h-screen grid-cols-1 bg-background-200 lg:grid-cols-[240px_minmax(0,1fr)]">
      <aside className="flex flex-col gap-5 border-r border-gray-alpha-400 bg-background-100 px-3 py-4">
        <div className="flex items-center gap-2.5 px-2 py-1">
          <Settings2 size={22} className="text-teal-700" />
          <div className="flex flex-col">
            <strong className="text-base font-semibold text-gray-1000">BoxFleet</strong>
            <span className="text-xs text-gray-700">Admin</span>
          </div>
        </div>
        <nav className="flex flex-col gap-1">
          {pages.map((item) => {
            const Icon = item.icon;
            return (
              <NavLink
                key={item.id}
                to={item.path}
                end={item.path === "/"}
                className={({ isActive }) => `flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm transition-colors ${
                  isActive
                    ? "bg-gray-alpha-200 font-semibold text-gray-1000"
                    : "text-gray-900 hover:bg-gray-alpha-100 hover:text-gray-1000"
                }`}
              >
                <Icon size={16} />
                <span>{item.label}</span>
              </NavLink>
            );
          })}
        </nav>
      </aside>
      <main className="min-w-0 px-6 py-5">
        <header className="mb-5 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <h1 className="text-2xl font-semibold tracking-tight text-gray-1000">
            {currentPage.label}
          </h1>
          <form
            className="flex flex-wrap items-center gap-2"
            onSubmit={(event) => {
              event.preventDefault();
              applyToken();
            }}
          >
            <Input
              type="password"
              size="sm"
              placeholder="Admin token"
              value={tokenInput}
              onChange={(event) => setTokenInput(event.target.value)}
              containerClassName="w-[220px]"
            />
            <Button
              type="submit"
              size="sm"
              variant="secondary"
              disabled={tokenInput.trim() === activeToken.trim()}
              prefix={<Check size={14} />}
            >
              Apply
            </Button>
            {activeToken ? (
              <Button type="button" size="sm" variant="tertiary" prefix={<LogOut size={14} />} onClick={logout}>
                Logout
              </Button>
            ) : null}
            <Button
              type="button"
              size="sm"
              variant="secondary"
              disabled={adminFetching}
              svgOnly
              onClick={() => void refresh()}
              title="Refresh"
            >
              <RefreshCw size={14} className={adminFetching ? "animate-spin" : ""} />
            </Button>
          </form>
        </header>
        {error ? (
          <div className="mb-4">
            <Note variant="error" size="md">{error instanceof Error ? error.message : "request failed"}</Note>
          </div>
        ) : null}
        {loading && !overview ? (
          <div className="flex items-center justify-center py-12">
            <LoadingDots size={6}>Loading BoxFleet data</LoadingDots>
          </div>
        ) : (
          <section className="flex flex-col gap-4">
            <Suspense fallback={<RouteLoading />}>
              <Routes>
                <Route
                  path="/"
                  element={
                    <OverviewPage
                      activeNodes={activeNodes}
                      activeUsers={activeUsers}
                      overview={overview}
                      trafficRows={trafficRows}
                      totalTraffic={totalTraffic}
                    />
                  }
                />
                <Route path="/nodes" element={<NodesPage nodes={overview?.nodes ?? []} request={request} refresh={refresh} />} />
                <Route path="/proxies" element={<ProxiesPage nodes={overview?.nodes ?? []} request={request} />} />
                <Route path="/users" element={<UsersPage refresh={refresh} request={request} users={overview?.users ?? []} />} />
                <Route path="/traffic" element={<TrafficPage rows={trafficRows} />} />
                <Route
                  path="/network-events"
                  element={
                    <NetworkEventsPage
                      nodes={overview?.nodes ?? []}
                      request={request}
                      users={overview?.users ?? []}
                    />
                  }
                />
                <Route path="/system-logs" element={<SystemLogsRoute authVersion={authVersion} request={request} />} />
                <Route path="*" element={<Navigate to="/" replace />} />
              </Routes>
            </Suspense>
          </section>
        )}
      </main>
    </div>
  );
}

function RouteLoading() {
  return (
    <div className="flex items-center justify-center py-10">
      <LoadingDots size={6}>Loading page</LoadingDots>
    </div>
  );
}

function SystemLogsRoute({ authVersion, request }: { authVersion: number; request: Requester }) {
  const systemLogsQuery = useQuery({
    queryKey: ["admin", "system-logs", authVersion],
    queryFn: () => request<SystemLogsResponse>("/api/admin/system-logs?limit=100")
  });
  if (systemLogsQuery.isLoading) {
    return <RouteLoading />;
  }
  if (systemLogsQuery.error) {
    return (
      <Note variant="error" size="md">
        {systemLogsQuery.error instanceof Error ? systemLogsQuery.error.message : "load system logs failed"}
      </Note>
    );
  }
  return <SystemLogsPage response={systemLogsQuery.data ?? { logs: [], note: "" }} />;
}

export default App;
