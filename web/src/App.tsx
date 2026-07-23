import { GearSixIcon } from "@phosphor-icons/react";
import { lazy, Suspense, useState } from "react";
import { useIsFetching, useQuery, useQueryClient } from "@tanstack/react-query";
import { Navigate, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { Banner, Loader, Sidebar, Text } from "@cloudflare/kumo";

import { AppPageHeader } from "@/components/app-page-header";
import { PublishStatusProvider } from "@/publish/publish-status";
import type { AdminRequest } from "@/publish/publish-status";
import { PublishDiffDialog } from "@/publish/publish-diff-dialog";
import { adminBasename, navGroups, pages, settingsNav } from "./navigation";
import type { NavItem } from "./navigation";
import type { Overview } from "./types";

const loadNetworkEventsPage = () => import("./pages/network-events");
const loadMihomoProfilesPage = () => import("./pages/mihomo-profiles");
const loadNodesPage = () => import("./pages/nodes");
const loadOverviewPage = () => import("./pages/overview");
const loadProxiesPage = () => import("./pages/proxies");
const loadSettingsPage = () => import("./pages/settings");
const loadSystemLogsPage = () => import("./pages/system-logs");
const loadUsersPage = () => import("./pages/users");

const routePreloaders: Partial<Record<string, () => Promise<unknown>>> = {
  "/": loadOverviewPage,
  "/nodes": loadNodesPage,
  "/proxies": loadProxiesPage,
  "/users": loadUsersPage,
  "/mihomo-profiles": loadMihomoProfilesPage,
  "/network-events": loadNetworkEventsPage,
  "/system-logs": loadSystemLogsPage,
  "/settings": loadSettingsPage
};

const NetworkEventsPage = lazy(() =>
  loadNetworkEventsPage().then((module) => ({ default: module.NetworkEventsPage }))
);
const MihomoProfilesPage = lazy(() =>
  loadMihomoProfilesPage().then((module) => ({ default: module.MihomoProfilesPage }))
);
const MihomoConfigurationPage = lazy(() =>
  loadMihomoProfilesPage().then((module) => ({ default: module.MihomoConfigurationPage }))
);
const NodesPage = lazy(() => loadNodesPage().then((module) => ({ default: module.NodesPage })));
const OverviewPage = lazy(() => loadOverviewPage().then((module) => ({ default: module.OverviewPage })));
const ProxiesPage = lazy(() => loadProxiesPage().then((module) => ({ default: module.ProxiesPage })));
const SettingsPage = lazy(() => loadSettingsPage().then((module) => ({ default: module.SettingsPage })));
const SystemLogsPage = lazy(() =>
  loadSystemLogsPage().then((module) => ({ default: module.SystemLogsPage }))
);
const UsersPage = lazy(() => loadUsersPage().then((module) => ({ default: module.UsersPage })));

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
    if (response.status === 204) {
      return undefined as T;
    }
    const contentType = response.headers.get("Content-Type") ?? "";
    if (contentType.includes("application/json")) {
      return (await response.json()) as T;
    }
    return (await response.text()) as T;
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

  return (
    <Sidebar.Provider collapsible="icon" defaultOpen className="h-svh bg-kumo-canvas">
      <AppSidebar />

      <main className="min-w-0 flex-1 overflow-y-auto">
        <PublishStatusProvider request={request}>
          <Suspense fallback={<PageLoader />}>
            <Routes>
              <Route path="/" element={<OverviewRoute request={request} authVersion={authVersion} />} />
              <Route path="/nodes" element={<NodesPage request={request} />} />
              <Route path="/proxies" element={<ProxiesPage request={request} />} />
              <Route path="/users" element={<UsersPage request={request} />} />
              <Route path="/mihomo-profiles" element={<MihomoProfilesPage request={request} />} />
              <Route path="/mihomo-profiles/new" element={<MihomoConfigurationPage request={request} />} />
              <Route path="/mihomo-profiles/:profile/edit" element={<MihomoConfigurationPage request={request} />} />
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
          </Suspense>

          <PublishDiffDialog />
        </PublishStatusProvider>
      </main>
    </Sidebar.Provider>
  );
}

function OverviewRoute({ request, authVersion }: { request: AdminRequest; authVersion: number }) {
  const overviewQuery = useQuery({
    queryKey: ["admin", "overview", authVersion],
    queryFn: () => request<Overview>("/api/admin/overview")
  });

  if (overviewQuery.error) {
    return (
      <div className="px-6 pt-6">
        <Banner
          variant="error"
          title={overviewQuery.error instanceof Error ? overviewQuery.error.message : "Request failed"}
        />
      </div>
    );
  }
  if (overviewQuery.isLoading && !overviewQuery.data) {
    return <PageLoader />;
  }
  return <OverviewPage overview={overviewQuery.data ?? null} />;
}

function PageLoader() {
  return (
    <div className="flex items-center justify-center py-16">
      <Loader size={20} />
    </div>
  );
}

function AppSidebar() {
  const location = useLocation();
  const navigate = useNavigate();

  const renderItem = (item: NavItem) => {
    const preload = () => {
      void routePreloaders[item.path]?.();
    };
    return (
      <Sidebar.MenuButton
        key={item.id}
        icon={item.icon}
        tooltip={item.label}
        active={item.path === "/" ? location.pathname === "/" : location.pathname.startsWith(item.path)}
        onPointerEnter={preload}
        onPointerDown={preload}
        onFocus={preload}
        onClick={() => navigate(item.path)}
      >
        {item.label}
      </Sidebar.MenuButton>
    );
  };

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
