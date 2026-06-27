import { GearSixIcon } from "@phosphor-icons/react";
import { useState } from "react";
import { useIsFetching, useQuery, useQueryClient } from "@tanstack/react-query";
import { Navigate, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { Banner, Loader, Sidebar, Text } from "@cloudflare/kumo";

import { AppPageHeader } from "@/components/app-page-header";
import { PublishStatusProvider } from "@/publish/publish-status";
import { PublishDiffDialog } from "@/publish/publish-diff-dialog";
import { adminBasename, navGroups, pages, settingsNav } from "./navigation";
import type { NavItem } from "./navigation";
import { NetworkEventsPage } from "./pages/network-events";
import { NodesPage } from "./pages/nodes";
import { OverviewPage } from "./pages/overview";
import { ProxiesPage } from "./pages/proxies";
import { SettingsPage } from "./pages/settings";
import { SystemLogsPage } from "./pages/system-logs";
import { UsersPage } from "./pages/users";
import type { Overview } from "./types";

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

  return (
    <Sidebar.Provider collapsible="icon" defaultOpen className="h-svh bg-kumo-canvas">
      <AppSidebar />

      <main className="min-w-0 flex-1 overflow-y-auto">
        <PublishStatusProvider request={request}>
          {error ? (
            <div className="px-6 pt-6">
              <Banner variant="error" title={error instanceof Error ? error.message : "Request failed"} />
            </div>
          ) : null}

          {loading && !overview ? (
            <div className="flex items-center justify-center py-16">
              <Loader size={20} />
            </div>
          ) : (
            <Routes>
              <Route path="/" element={<OverviewPage overview={overview} />} />
              <Route path="/nodes" element={<NodesPage request={request} />} />
              <Route path="/proxies" element={<ProxiesPage request={request} />} />
              <Route path="/users" element={<UsersPage request={request} />} />
              <Route path="/traffic" element={<ComingSoon />} />
              <Route path="/network-events" element={<NetworkEventsPage request={request} />} />
              <Route path="/system-logs" element={<SystemLogsPage request={request} />} />
              <Route
                path="/settings"
                element={
                  <SettingsPage
                    tokenInput={tokenInput}
                    setTokenInput={setTokenInput}
                    activeToken={activeToken}
                    applyToken={applyToken}
                    logout={logout}
                    refresh={() => void refresh()}
                    refreshing={adminFetching}
                  />
                }
              />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          )}

          <PublishDiffDialog />
        </PublishStatusProvider>
      </main>
    </Sidebar.Provider>
  );
}

function AppSidebar() {
  const location = useLocation();
  const navigate = useNavigate();

  const renderItem = (item: NavItem) => (
    <Sidebar.MenuButton
      key={item.id}
      icon={item.icon}
      tooltip={item.label}
      active={item.path === "/" ? location.pathname === "/" : location.pathname.startsWith(item.path)}
      onClick={() => navigate(item.path)}
    >
      {item.label}
    </Sidebar.MenuButton>
  );

  return (
    <Sidebar>
      <Sidebar.Header>
        <div className="flex min-w-0 translate-x-[11px] items-center gap-2.5 py-1 transition-transform duration-(--sidebar-animation-duration) ease-(--sidebar-easing) motion-reduce:transition-none group-data-[state=collapsed]/sidebar:translate-x-[5.5px]">
          <GearSixIcon size={22} weight="duotone" className="shrink-0 text-kumo-default" />
          <div className="flex min-w-0 transition-[opacity,transform] duration-(--sidebar-animation-duration) ease-(--sidebar-easing) motion-reduce:transition-none group-data-[state=collapsed]/sidebar:translate-x-2 group-data-[state=collapsed]/sidebar:opacity-0">
            <Text bold as="span" truncate>
              BoxFleet Admin
            </Text>
          </div>
        </div>
      </Sidebar.Header>

      <Sidebar.Content>
        {navGroups.map((group, index) => (
          <Sidebar.Group key={group.label ?? `group-${index}`}>
            {group.label ? <Sidebar.GroupLabel>{group.label}</Sidebar.GroupLabel> : null}
            <Sidebar.Menu>{group.items.map(renderItem)}</Sidebar.Menu>
          </Sidebar.Group>
        ))}

        <Sidebar.Separator />

        <Sidebar.Group>
          <Sidebar.Menu>{renderItem(settingsNav)}</Sidebar.Menu>
        </Sidebar.Group>
      </Sidebar.Content>

      <Sidebar.Footer>
        <Sidebar.Trigger />
      </Sidebar.Footer>
    </Sidebar>
  );
}

function ComingSoon() {
  const location = useLocation();
  const page = pages.find((item) => item.path === location.pathname);
  return (
    <AppPageHeader title={page?.label ?? "Page"} description="This page is being rewritten on native Kumo.">
      <Text variant="secondary">Coming soon.</Text>
    </AppPageHeader>
  );
}

export default App;
