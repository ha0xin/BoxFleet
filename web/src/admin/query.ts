/**
 * Shared auto-refresh cadences so every page stays live without a manual
 * reload. TanStack Query pauses interval refetches while the tab is hidden
 * (`refetchIntervalInBackground` defaults to false), so these are focused-tab
 * costs only.
 */
export const refreshIntervals = {
  /** State driven by node heartbeats: nodes, users, proxies, logs. */
  live: 15_000,
  /** Telemetry feeds and aggregates: traffic series, network events. */
  telemetry: 30_000,
  /** Slow-moving inventories and trend charts. */
  slow: 60_000
} as const;

export function queryString(params: Record<string, string | number | boolean | undefined>): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === "") continue;
    query.set(key, String(value));
  }
  const text = query.toString();
  return text ? `?${text}` : "";
}

export const adminKeys = {
  root: ["admin"] as const,
  overview: (authVersion: number) => ["admin", "overview", authVersion] as const,
  users: (deleted = false) => ["admin", "users", deleted] as const,
  nodes: ["admin", "nodes-all"] as const,
  proxies: ["admin", "proxies-all"] as const,
  paths: ["admin", "paths"] as const,
  configChanges: ["admin", "config-changes"] as const,
  publishNodes: ["admin", "publish-nodes"] as const,
  mihomoProfiles: ["admin", "mihomo-profiles"] as const,
  mihomoTemplates: ["admin", "mihomo-rewrite-templates"] as const,
  mihomoProfile: (id: string) => ["admin", "mihomo-profile", id] as const,
  subscription: (kind: "user" | "mihomo-profile", id: string) => ["admin", "subscription", kind, id] as const,
  trafficUsers: ["admin", "traffic-users"] as const,
  systemLogs: (...state: readonly unknown[]) => ["admin", "system-logs", ...state] as const,
  networkEvents: (filters: object) => ["admin", "network-events", filters] as const,
  trafficSeries: (filters: object) => ["admin", "traffic-series", filters] as const,
  networkEventSeries: (filters: object) => ["admin", "network-event-series", filters] as const,
  networkEventServices: (filters: object) => ["admin", "network-event-services", filters] as const,
  networkEventHosts: (filters: object) => ["admin", "network-event-hosts", filters] as const,
  serviceOverrides: ["admin", "service-overrides"] as const,
  userAccess: (name: string) => ["admin", "user-access", name] as const,
  userConnection: (name: string) => ["admin", "user-connection-info", name] as const,
  nodesPage: (...state: readonly unknown[]) => ["admin", "nodes-page", ...state] as const,
  proxiesPage: (...state: readonly unknown[]) => ["admin", "proxies-page", ...state] as const,
  usersPage: (...state: readonly unknown[]) => ["admin", "users-page", ...state] as const,
  release: ["admin", "release"] as const,
  nodeOperation: (node: string, operation: string) => ["admin", "node-operation", node, operation] as const,
  nodeUpdateCampaign: (campaign: string) => ["admin", "node-update-campaign", campaign] as const
};
