export type AdminNodeStatus = "pending" | "active" | "disabled" | "degraded";
export type AdminNodeApplyStatus = "pending" | "applied" | "failed" | "rolled_back";

export type AdminNode = {
  id: string;
  name: string;
  public_host: string;
  api_base_url: string;
  status: AdminNodeStatus;
  sing_box_version: string;
  last_seen_at: string;
  target_version?: string;
  current_version?: string;
  apply_status?: AdminNodeApplyStatus;
  apply_error?: string;
  latest_heartbeat?: string;
  agent_version?: string;
  has_active_token?: boolean;
};

export type ConfigChange = {
  node: string;
  target_hash: string;
  rendered_hash: string;
  target_version: string;
  target_config: string;
  rendered_config: string;
};

export type ConfigChangesResponse = { changed: ConfigChange[] };

export type PublishResult = {
  node: string;
  id?: string;
  version: number | string;
  hash?: string;
  created?: boolean;
};

export type PublishResponse = { published: PublishResult[] };

export type AdminNodeBootstrap = {
  node: AdminNode;
  bootstrap_string: string;
  install_script_url: string;
};

export type AdminNodesResponse = {
  nodes: AdminNode[];
  total: number;
  limit: number;
  offset: number;
};

export type AdminProxy = {
  id: string;
  node_name: string;
  name: string;
  protocol: string;
  listen: string;
  listen_port: number;
  transport: string;
  enabled: boolean;
  traffic_multiplier: number;
  settings_json: string;
  inbound_rules_json: string;
  outbound_rules_json: string;
  route_rules_json: string;
  created_at: string;
  updated_at: string;
};

export type AdminProxiesResponse = {
  proxies: AdminProxy[];
  total: number;
  limit: number;
  offset: number;
};

export type AdminUser = {
  id: string;
  name: string;
  display_name: string;
  status: string;
  global_quota_bytes: number;
  expire_at: string;
  proxy_count: number;
};

export type AdminProxyAccess = {
  id: string;
  user_name: string;
  node_name: string;
  proxy_name: string;
  protocol: string;
  listen: string;
  listen_port: number;
  transport: string;
  auth_name: string;
  enabled: boolean;
  quota_bytes: number;
  traffic_multiplier?: number;
  proxy_multiplier: number;
  created_at: string;
  updated_at: string;
};

export type UserConnectionInfo = {
  user: string;
  nodes: Array<{
    user: string;
    node: string;
    proxies: Array<{
      name: string;
      type: string;
      server: string;
      server_port: number;
      uuid: string;
      flow: string;
      server_name: string;
      public_key: string;
      short_id: string;
    }>;
  }>;
};

export type TrafficRow = {
  user_name: string;
  direction: string;
  raw_bytes: number;
  billable_bytes: number;
};

export type NetworkEvent = {
  node_name: string;
  user_name: string;
  auth_name: string;
  source_ip: string;
  target_host: string;
  target_port: number;
  action: string;
  raw_message: string;
  count: number;
  window_start: string;
  window_end: string;
  created_at: string;
};

export type NetworkEventsResponse = {
  events: NetworkEvent[];
  total: number;
  limit: number;
  offset: number;
};

export type AdminSettings = {
  network_event_retention_days: number;
};

export type SystemLog = {
  node: string;
  service: string;
  level: string;
  message: string;
  observed_at: string;
  ingested_at: string;
};

export type Overview = {
  nodes: AdminNode[];
  users: AdminUser[];
  traffic: TrafficRow[];
  system_logs: SystemLog[];
  system_log_note: string;
  release: {
    repo: string;
    boxfleet_version: string;
    sing_box_version: string;
  };
};

export type SystemLogsResponse = {
  logs: SystemLog[];
  note: string;
};

export type Page = "overview" | "nodes" | "proxies" | "users" | "traffic" | "network-events" | "system-logs" | "settings";
