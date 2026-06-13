import { Check, LogOut, RefreshCw, Settings2 } from "lucide-react";
import { lazy, Suspense, useMemo, useState } from "react";
import { useIsFetching, useQuery, useQueryClient } from "@tanstack/react-query";
import { Navigate, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { Sidebar } from "@cloudflare/kumo";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { LoadingDots } from "@/components/ui/loading-dots";
import { Note } from "@/components/ui/note";
import { PageHeader } from "@/components/ui/page-header/page-header";

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
  const navigate = useNavigate();
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
    <Sidebar.Provider collapsible="none">
      <Sidebar>
        <Sidebar.Header>
          <div className="flex items-center gap-2.5 px-1 py-1">
            <Settings2 size={22} className="text-gray-1000" />
            <div className="flex flex-col">
              <strong className="text-base font-semibold text-gray-1000">BoxFleet</strong>
              <span className="text-xs text-gray-700">Admin</span>
            </div>
          </div>
        </Sidebar.Header>
        <Sidebar.Content>
          <Sidebar.Menu>
            {pages.map((item) => (
              <Sidebar.MenuButton
                key={item.id}
                icon={item.icon}
                active={
                  item.path === "/"
                    ? location.pathname === "/"
                    : location.pathname.startsWith(item.path)
                }
                onClick={() => navigate(item.path)}
              >
                {item.label}
              </Sidebar.MenuButton>
            ))}
          </Sidebar.Menu>
        </Sidebar.Content>
        <Sidebar.Footer>
          <form
            className="flex flex-col gap-2 px-1 py-1"
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
              containerClassName="w-full"
            />
            <div className="flex items-center gap-2">
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
                className="ml-auto"
              >
                <RefreshCw size={14} className={adminFetching ? "animate-spin" : ""} />
              </Button>
            </div>
          </form>
        </Sidebar.Footer>
      </Sidebar>
      <main className="min-w-0 flex-1 px-6 py-5">
        <PageHeader title={currentPage.label} className="mb-5" />
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
    </Sidebar.Provider>
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
