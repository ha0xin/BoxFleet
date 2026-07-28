export type AdminNodeStatus = "pending" | "active" | "disabled" | "degraded";
export type AdminNodeApplyStatus = "pending" | "applied" | "failed" | "rolled_back";

export type AdminNodeHost = {
  id: string;
  host: string;
  tag: string;
  selected: boolean;
};

export type AdminNode = {
  id: string;
  name: string;
  public_host: string;
  hosts?: AdminNodeHost[];
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
  agent_goos?: string;
  agent_goarch?: string;
  capabilities?: string[];
  active_operation?: NodeOperation;
  has_active_token?: boolean;
  deleted_at: string;
};

export type NodeOperationStatus = "queued" | "running" | "succeeded" | "failed" | "cancelled" | "expired";

export type NodeOperation = {
  id: string;
  node_id: string;
  kind: string;
  status: NodeOperationStatus;
  phase: string;
  payload: Record<string, unknown>;
  result: Record<string, unknown>;
  idempotency_key: string;
  required_capabilities: string[];
  attempt: number;
  lease_expires_at?: string;
  cancel_requested: boolean;
  requested_at: string;
  started_at?: string;
  finished_at?: string;
  updated_at: string;
  error?: string;
};

export type NodeOperationEvent = {
  id: string;
  operation_id: string;
  attempt: number;
  sequence: number;
  status: NodeOperationStatus;
  phase: string;
  message?: string;
  details: Record<string, unknown>;
  result: Record<string, unknown>;
  error?: string;
  reported_at: string;
};

export type NodeOperationDetail = {
  operation: NodeOperation;
  events: NodeOperationEvent[];
};

export type AdminRelease = {
  repo: string;
  boxfleet_version: string;
  agent_version: string;
  sing_box_version: string;
  updates_enabled: boolean;
  update_error?: string;
};

export type NodeUpdateCampaignMember = {
  campaign_id: string;
  node_id: string;
  node_name: string;
  position: number;
  batch_number: number;
  kind: string;
  operation_id?: string;
  status: string;
  error?: string;
  started_at?: string;
  finished_at?: string;
  updated_at: string;
};

export type NodeUpdateCampaign = {
  id: string;
  release: string;
  components: string[];
  status: "queued" | "running" | "paused" | "succeeded" | "cancelled";
  idempotency_key: string;
  batch_size: number;
  current_batch: number;
  requested_at: string;
  started_at?: string;
  finished_at?: string;
  updated_at: string;
  error?: string;
};

export type NodeUpdateCampaignDetail = {
  campaign: NodeUpdateCampaign;
  members: NodeUpdateCampaignMember[];
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
  direct_publish: boolean;
  short_id: string;
  settings_json: string;
  inbound_rules_json: string;
  outbound_rules_json: string;
  route_rules_json: string;
  created_at: string;
  updated_at: string;
  deleted_at: string;
};

export type AdminProxiesResponse = {
  proxies: AdminProxy[];
  total: number;
  limit: number;
  offset: number;
};

export type AdminPath = {
  id: string;
  name: string;
  display_name: string;
  endpoint_id: string;
  proxy_id: string;
  proxy_name: string;
  node_name: string;
  host_id: string;
  host: string;
  host_tag: string;
  dialer_path_id: string;
  enabled: boolean;
  visibility: "selectable" | "dependency";
  managed: boolean;
  sort_order: number;
  created_at: string;
  updated_at: string;
};

export type AdminPathAccess = {
  id: string;
  path_id: string;
  proxy_user_id: string;
  enabled: boolean;
  deleted_at: string;
  created_at: string;
  updated_at: string;
};

export type AdminUser = {
  id: string;
  name: string;
  display_name: string;
  status: string;
  global_quota_bytes: number;
  expire_at: string;
  proxy_count: number;
  deleted_at: string;
};

// The status the server derives, filters, and sorts on. It differs from
// AdminUser["status"]: quota exhaustion and expiry are states the stored column
// only learns about later, so the derived value is what a row must badge.
export type AdminUserEffectiveStatus = "active" | "disabled" | "expired" | "quota_exceeded" | "deleted";

// One row of the paged inventory: every AdminUser field, plus the derived status
// and the traffic totals the server resolved in the same query. Traffic arrives
// with the page precisely so a paged table never needs the full
// /api/admin/traffic/users inventory alongside it.
export type AdminUserRow = AdminUser & {
  effective_status: AdminUserEffectiveStatus;
  traffic: TrafficVolume;
};

export type AdminUsersResponse = {
  users: AdminUserRow[];
  total: number;
  limit: number;
  offset: number;
};

// Sort keys the server whitelists. Anything else falls back to name order.
export type AdminUserSort =
  | "name"
  | "display_name"
  | "status"
  | "traffic"
  | "quota"
  | "proxy_count"
  | "expire_at"
  | "created_at"
  | "updated_at";

export type AdminProxyCredential = {
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
  deleted_at: string;
};

export type UserConnectionInfo = {
  user: string;
  nodes: Array<{
    user: string;
    node: string;
    proxies: Array<{
      name: string;
      proxy_name: string;
      host_tag: string;
      type: string;
      server: string;
      server_port: number;
      uuid: string;
      flow: string;
      server_name: string;
      public_key: string;
      short_id: string;
      // Shadowsocks 2022 only; omitted by the server for VLESS-Reality.
      cipher?: string;
      password?: string;
      // Set when the Path is chained through another Path.
      dialer_proxy?: string;
    }>;
  }>;
};

export type AdminSubscription = {
  active: boolean;
  url: string;
  mihomo_url?: string;
  provider_url?: string;
  created_at: string;
  last_used_at: string;
};

export type MihomoRewrite = {
  id: string;
  template_id?: string;
  name: string;
  kind: "yaml" | "javascript";
  content: string;
  enabled: boolean;
};

export type MihomoProfileDocument = { rewrites: MihomoRewrite[] };

export type MihomoProfile = {
  id: string;
  name: string;
  description: string;
  proxy_user_id: string;
  proxy_user_name: string;
  document: MihomoProfileDocument;
  created_at: string;
  updated_at: string;
};

export type MihomoRewriteTemplate = {
  id: string;
  name: string;
  description: string;
  kind: "yaml" | "javascript";
  content: string;
  built_in: boolean;
  created_at: string;
  updated_at: string;
};

export type MihomoProfileSubscription = {
  active: boolean;
  url: string;
  created_at: string;
  last_used_at: string;
};

export type MihomoDiagnostic = {
  severity: "error" | "warning";
  code: string;
  path?: string;
  message: string;
};

export type MihomoLogEntry = { level: string; message: string };

export type MihomoPreview = {
  yaml: string;
  logs: MihomoLogEntry[];
  diagnostics: MihomoDiagnostic[];
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

// Bucketed telemetry. The server owns bucket derivation and zero-fill; the
// client renders `points` as delivered and never re-buckets. Hour buckets are
// UTC; day buckets honour the `offset_minutes` the client sends.
export type SeriesBucket = "hour" | "day";

export type TrafficSeriesGroup = "total" | "user" | "node";

export type TrafficVolume = {
  uplink_raw_bytes: number;
  uplink_billable_bytes: number;
  downlink_raw_bytes: number;
  downlink_billable_bytes: number;
};

export type TrafficPoint = TrafficVolume & { bucket_start: string };

export type TrafficSeries = {
  key: string;
  label: string;
  points: TrafficPoint[];
  totals: TrafficVolume;
};

export type TrafficSeriesResponse = {
  bucket: SeriesBucket;
  offset_minutes: number;
  start: string;
  end: string;
  group: TrafficSeriesGroup;
  series: TrafficSeries[];
  truncated: boolean;
};

export type NetworkEventSeriesGroup = "total" | "action" | "node" | "user";

export type NetworkEventPoint = {
  bucket_start: string;
  count: number;
};

export type NetworkEventSeries = {
  key: string;
  label: string;
  points: NetworkEventPoint[];
  total: number;
};

export type NetworkEventActionCount = {
  action: string;
  count: number;
};

export type NetworkEventSeriesResponse = {
  bucket: SeriesBucket;
  offset_minutes: number;
  start: string;
  end: string;
  group: NetworkEventSeriesGroup;
  series: NetworkEventSeries[];
  actions: NetworkEventActionCount[];
  truncated: boolean;
};

export type ServiceUsageGroup = "service" | "category";

// Connections per service — log events carry no byte counts, so this is never
// traffic. Label it "connections" wherever it is rendered.
export type ServiceUsageRow = {
  key: string;
  label: string;
  category: string;
  connections: number;
  hosts: number;
};

export type ServiceUsageResponse = {
  start: string;
  end: string;
  group: ServiceUsageGroup;
  rows: ServiceUsageRow[];
  other: ServiceUsageRow;
  total_connections: number;
  total_hosts: number;
  truncated: boolean;
  catalog_version: string;
};

export type ServiceClassificationSource = "override" | "catalog" | "publicsuffix" | "ip" | "unknown";

export type NetworkEventHost = {
  host: string;
  service: string;
  service_label: string;
  category: string;
  source: ServiceClassificationSource;
  connections: number;
  last_seen: string;
};

export type NetworkEventHostsResponse = {
  hosts: NetworkEventHost[];
  total: number;
  limit: number;
  offset: number;
  truncated: boolean;
};

// Connection telemetry — the second network-event producer, sing-box 1.14's
// daemon gRPC stream. It is opt-in per node and off by default: the production
// fleet runs 1.13, where the `service.api` config block does not parse at all.
// A node that streams produces these rows *instead of nothing*, not instead of
// log_events, so the two sources are surfaced separately and never merged.
//
// Every byte figure below is an ESTIMATE. sing-box drops events silently when a
// subscriber buffer fills, evicts its closed-connection ring at 1000 entries,
// and resets connection ids on restart. Render them with `ConnectionCoverage`
// alongside, and never label them "traffic" — billing reads the V2Ray counters
// behind `/traffic/series`.
export type ConnectionVolume = {
  /** Sessions that opened in the bucket. Sum this, never `connections_closed`. */
  connections_opened: number;
  connections_closed: number;
  uplink_bytes: number;
  downlink_bytes: number;
  /** Precomputed uplink + downlink so no client re-derives the ranked figure. */
  total_bytes: number;
  duration_ms_total: number;
};

// The collector's own loss telemetry, carried on every response with bytes.
export type ConnectionCoverage = {
  connections_observed: number;
  connections_attributed: number;
  connections_unattributed: number;
  connections_orphaned: number;
  stream_resets: number;
  dropped_buckets: number;
  bytes_observed: number;
  bytes_attributed: number;
  /** Attributed / observed bytes in [0,1]; an empty window reports 1. */
  attribution_ratio: number;
  reports: number;
};

export type ConnectionEvent = {
  node_name: string;
  user_name: string;
  auth_name: string;
  source_ip: string;
  target_host: string;
  target_port: number;
  /** Sniffed name. Empty on BoxFleet's rendered config; the host is target_host. */
  domain: string;
  network: string;
  ip_version: number;
  protocol: string;
  inbound: string;
  inbound_type: string;
  rule: string;
  outbound: string;
  outbound_type: string;
  /** Outbound chain flattened with ">". */
  chain: string;
  connections_opened: number;
  connections_closed: number;
  uplink_bytes: number;
  downlink_bytes: number;
  duration_ms_total: number;
  bucket_start: string;
  window_start: string;
  window_end: string;
};

export type ConnectionEventsResponse = {
  events: ConnectionEvent[];
  total: number;
  limit: number;
  offset: number;
};

export type ConnectionPoint = ConnectionVolume & { bucket_start: string };

export type ConnectionSeriesResponse = {
  bucket: SeriesBucket;
  offset_minutes: number;
  start: string;
  end: string;
  points: ConnectionPoint[];
  totals: ConnectionVolume;
  coverage: ConnectionCoverage;
};

export type ConnectionHostSort = "bytes" | "connections";

export type ConnectionHost = ConnectionVolume & { host: string };

export type ConnectionHostsResponse = {
  sort: ConnectionHostSort;
  hosts: ConnectionHost[];
  /** Unclipped window totals; the share denominator, independent of `limit`. */
  totals: ConnectionVolume;
  distinct_hosts: number;
  limit: number;
  truncated: boolean;
  coverage: ConnectionCoverage;
};

// Which nodes actually stream. The secret is a full control-plane credential
// for sing-box's daemon API and is deliberately absent from this shape.
export type ConnectionTelemetryNode = {
  node_name: string;
  listen_address: string;
  listen_port: number;
};

export type ConnectionTelemetryNodesResponse = {
  nodes: ConnectionTelemetryNode[];
};

export type DomainServiceOverride = {
  suffix: string;
  service: string;
  label: string;
  category: string;
  created_at: string;
  updated_at: string;
};

export type DomainServiceOverrideInput = {
  suffix: string;
  service: string;
  label: string;
  category: string;
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
    agent_version: string;
    sing_box_version: string;
    updates_enabled?: boolean;
    update_error?: string;
  };
};

// `note` predates pagination and is always "" today; it stays so the overview's
// system_log_note and this payload keep the same vocabulary.
export type SystemLogsResponse = {
  logs: SystemLog[];
  // Every service that has ever reported, not just the ones on this page. Feed
  // the service filter from here; a page of rows cannot enumerate the options,
  // and options must not move when the filter is applied. Node options come
  // from /api/admin/nodes for the same reason.
  services: string[];
  total: number;
  limit: number;
  offset: number;
  note: string;
};

// Level is bucketed server-side the way the page badges it: journald levels are
// free text, so "error" also matches "err" and "LEVEL_FATAL", and "info" is the
// residual bucket. An omitted filter means "all"; there is no "all" sentinel,
// because a node may legitimately be named that.
export type SystemLogLevelFilter = "error" | "warn" | "info" | "debug";

export type SystemLogSort = "observed_at" | "node" | "service" | "level" | "message" | "ingested_at";

export type Page =
  | "overview"
  | "nodes"
  | "proxies"
  | "paths"
  | "users"
  | "mihomo-profiles"
  | "traffic"
  | "network-events"
  | "system-logs"
  | "settings";
