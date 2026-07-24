import { GearSixIcon } from "@phosphor-icons/react";
import { lazy, Suspense, useCallback, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Navigate, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { Banner, Button, Loader, Sidebar, Text } from "@cloudflare/kumo";

import { AppPageHeader } from "@/components/app-page-header";
import { AdminApiProvider, useAdminApi } from "@/admin/api";
import { adminKeys } from "@/admin/query";
import { PublishStatusProvider } from "@/publish/publish-status";
import { PublishDiffDialog } from "@/publish/publish-diff-dialog";
import { navGroups, pages, settingsNav } from "./navigation";
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

function App() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [activeToken, setActiveToken] = useState(() => localStorage.getItem("boxfleet.adminToken") ?? "");
  const [tokenInput, setTokenInput] = useState(activeToken);
  const [authVersion, setAuthVersion] = useState(0);
  const [authRequired, setAuthRequired] = useState(false);
  const authRequiredRef = useRef(false);
  const handleUnauthorized = useCallback(() => {
    if (authRequiredRef.current) return;
    authRequiredRef.current = true;
    setAuthRequired(true);
    navigate("/settings", { replace: true });
  }, [navigate]);

  async function refresh() {
    await queryClient.invalidateQueries({ queryKey: adminKeys.root });
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
    authRequiredRef.current = false;
    setAuthRequired(false);
    setAuthVersion((value) => value + 1);
  }

  function logout() {
    setTokenInput("");
    setActiveToken("");
    localStorage.removeItem("boxfleet.adminToken");
    queryClient.clear();
    authRequiredRef.current = true;
    setAuthRequired(true);
    setAuthVersion((value) => value + 1);
  }

  return (
    <AdminApiProvider token={activeToken} onUnauthorized={handleUnauthorized}>
      <AdminApp
        tokenInput={tokenInput}
        setTokenInput={setTokenInput}
        activeToken={activeToken}
        authVersion={authVersion}
        applyToken={applyToken}
        logout={logout}
        refresh={() => void refresh()}
        authRequired={authRequired}
      />
    </AdminApiProvider>
  );
}

function AdminApp({
  tokenInput,
  setTokenInput,
  activeToken,
  authVersion,
  applyToken,
  logout,
  refresh,
  authRequired
}: {
  tokenInput: string;
  setTokenInput: (value: string) => void;
  activeToken: string;
  authVersion: number;
  applyToken: () => void;
  logout: () => void;
  refresh: () => void;
  authRequired: boolean;
}) {
  return (
    <Sidebar.Provider collapsible="icon" defaultOpen className="h-svh bg-kumo-canvas">
      <AppSidebar />

      <main className="min-w-0 flex-1 overflow-y-auto">
        <PublishStatusProvider>
          {authRequired ? (
            <div className="px-4 pt-4 md:px-8">
              <Banner
                variant="error"
                title="Admin authentication required"
                description="The saved token was rejected. Enter a valid admin token in Settings to reconnect."
                action={
                  <Button variant="secondary" onClick={() => document.getElementById("admin-token-input")?.focus()}>
                    Enter token
                  </Button>
                }
              />
            </div>
          ) : null}
          <Suspense fallback={<PageLoader />}>
            <Routes>
              <Route path="/" element={<OverviewRoute authVersion={authVersion} />} />
              <Route path="/nodes" element={<NodesPage />} />
              <Route path="/proxies" element={<ProxiesPage />} />
              <Route path="/users" element={<UsersPage />} />
              <Route path="/mihomo-profiles" element={<MihomoProfilesPage />} />
              <Route path="/mihomo-profiles/new" element={<MihomoConfigurationPage />} />
              <Route path="/mihomo-profiles/:profile/edit" element={<MihomoConfigurationPage />} />
              <Route path="/traffic" element={<ComingSoon />} />
              <Route path="/network-events" element={<NetworkEventsPage />} />
              <Route path="/system-logs" element={<SystemLogsPage />} />
              <Route
                path="/settings"
                element={
                  <SettingsPage
                    tokenInput={tokenInput}
                    setTokenInput={setTokenInput}
                    activeToken={activeToken}
                    applyToken={applyToken}
                    logout={logout}
                    refresh={refresh}
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

function OverviewRoute({ authVersion }: { authVersion: number }) {
  const { request } = useAdminApi();
  const overviewQuery = useQuery({
    queryKey: adminKeys.overview(authVersion),
    queryFn: () => request<Overview>("/api/admin/overview"),
    refetchInterval: 15_000,
    refetchOnWindowFocus: true
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
