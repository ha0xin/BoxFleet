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
  configChanges: ["admin", "config-changes"] as const,
  publishNodes: ["admin", "publish-nodes"] as const,
  mihomoProfiles: ["admin", "mihomo-profiles"] as const,
  mihomoTemplates: ["admin", "mihomo-rewrite-templates"] as const,
  mihomoProfile: (id: string) => ["admin", "mihomo-profile", id] as const,
  subscription: (kind: "user" | "mihomo-profile", id: string) => ["admin", "subscription", kind, id] as const,
  trafficUsers: ["admin", "traffic-users"] as const,
  systemLogs: (limit: number) => ["admin", "system-logs", limit] as const,
  networkEvents: (filters: object) => ["admin", "network-events", filters] as const,
  userAccess: (name: string) => ["admin", "user-access", name] as const,
  userConnection: (name: string) => ["admin", "user-connection-info", name] as const,
  nodesPage: (...state: readonly unknown[]) => ["admin", "nodes-page", ...state] as const,
  proxiesPage: (...state: readonly unknown[]) => ["admin", "proxies-page", ...state] as const,
  release: ["admin", "release"] as const,
  nodeOperation: (node: string, operation: string) => ["admin", "node-operation", node, operation] as const,
  nodeUpdateCampaign: (campaign: string) => ["admin", "node-update-campaign", campaign] as const
};
