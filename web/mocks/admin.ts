import type { Connect, Plugin } from "vite";

import type {
  AdminNode,
  AdminNodeBootstrap,
  AdminNodesResponse,
  AdminPath,
  AdminPathAccess,
  AdminProxy,
  AdminProxyCredential,
  AdminProxiesResponse,
  AdminSubscription,
  AdminUser,
  AdminUserEffectiveStatus,
  AdminUserRow,
  AdminUsersResponse,
  MihomoPreview,
  MihomoProfile,
  MihomoProfileDocument,
  MihomoProfileSubscription,
  MihomoRewriteTemplate,
  ConnectionCoverage,
  ConnectionEvent,
  ConnectionEventsResponse,
  ConnectionHost,
  ConnectionHostSort,
  ConnectionHostsResponse,
  ConnectionPoint,
  ConnectionSeriesResponse,
  ConnectionTelemetryNodesResponse,
  ConnectionVolume,
  DomainServiceOverride,
  NetworkEvent,
  NetworkEventHost,
  NetworkEventHostsResponse,
  NetworkEventSeriesGroup,
  NetworkEventSeriesResponse,
  NetworkEventsResponse,
  NodeOperation,
  NodeOperationDetail,
  NodeUpdateCampaignDetail,
  Overview,
  SeriesBucket,
  ServiceClassificationSource,
  ServiceUsageGroup,
  ServiceUsageResponse,
  ServiceUsageRow,
  SystemLog,
  SystemLogsResponse,
  TrafficPoint,
  TrafficRow,
  TrafficSeriesGroup,
  TrafficSeriesResponse,
  TrafficVolume,
  UserConnectionInfo
} from "../src/types";

// Dev-only fixture data so `npm run dev` shows a populated UI without a running
// boxfleet-server. This file is never bundled into production — it is only used
// by the Vite dev server middleware in vite.config.ts.

const now = Date.now();
const iso = (msAgo: number) => new Date(now - msAgo).toISOString();
const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;
const GiB = 1024 ** 3;

const mockOperations = new Map<string, NodeOperationDetail>();
const mockCampaigns = new Map<string, NodeUpdateCampaignDetail>();

function completedOperation(nodeName: string): NodeOperationDetail {
  const timestamp = new Date().toISOString();
  const operation: NodeOperation = {
    id: `op_mock_${Date.now()}`,
    node_id: nodes.find((node) => node.name === nodeName)?.id ?? nodeName,
    kind: "update",
    status: "succeeded",
    phase: "completed",
    payload: {},
    result: {},
    idempotency_key: `mock:${nodeName}:${Date.now()}`,
    required_capabilities: [],
    attempt: 1,
    cancel_requested: false,
    requested_at: timestamp,
    started_at: timestamp,
    finished_at: timestamp,
    updated_at: timestamp
  };
  const detail = { operation, events: [] };
  mockOperations.set(operation.id, detail);
  return detail;
}

function completedCampaign(nodeNames: string[]): NodeUpdateCampaignDetail {
  const timestamp = new Date().toISOString();
  const id = `campaign_mock_${Date.now()}`;
  const detail: NodeUpdateCampaignDetail = {
    campaign: {
      id,
      release: "mock",
      components: ["agent", "sing_box"],
      status: "succeeded",
      idempotency_key: `mock:${id}`,
      batch_size: Math.max(nodeNames.length, 1),
      current_batch: 0,
      requested_at: timestamp,
      started_at: timestamp,
      finished_at: timestamp,
      updated_at: timestamp
    },
    members: nodeNames.map((nodeName, position) => ({
      campaign_id: id,
      node_id: nodes.find((node) => node.name === nodeName)?.id ?? nodeName,
      node_name: nodeName,
      position,
      batch_number: 0,
      kind: "update",
      status: "succeeded",
      updated_at: timestamp,
      started_at: timestamp,
      finished_at: timestamp
    }))
  };
  mockCampaigns.set(id, detail);
  return detail;
}

// Mirrors internal/model.Capability*; a current agent reports all of them on
// every heartbeat and the Nodes page gates managed updates on them.
const agentCapabilities = [
  "operations.v1",
  "update.agent.v1",
  "update.sing_box.v1",
  "download.streaming.v1",
  "install.versioned.v1",
  "restart_resume.agent.v1",
  "rollback.sing_box.v1"
] as const;

// The server derives a stable id per stored host row (see legacyNodeHostID), so
// reordering or removing a sibling never renumbers the survivors. Mirror that:
// Paths stores host_id, and an id that shifts under an edit breaks the link.
function mockHostID(nodeID: string, host: string): string {
  return `host_${nodeID}_${host.replace(/[^a-z0-9]+/gi, "_")}`;
}

const nodes: AdminNode[] = [
  {
    id: "node_tokyo",
    name: "tokyo",
    public_host: "tokyo.example.net",
    hosts: [
      { id: "host_tokyo_1", host: "tokyo.example.net", tag: "", selected: true },
      { id: "host_tokyo_2", host: "203.0.113.10", tag: "ipv4", selected: true },
      { id: "host_tokyo_3", host: "2606:4700::6810:84e5", tag: "ipv6", selected: false }
    ],
    api_base_url: "https://203.0.113.10:18080",
    status: "active",
    sing_box_version: "1.9.3",
    last_seen_at: iso(20_000),
    target_version: "v12",
    current_version: "v12",
    apply_status: "applied",
    latest_heartbeat: iso(20_000),
    agent_version: "0.4.1",
    capabilities: [...agentCapabilities],
    has_active_token: true,
    deleted_at: ""
  },
  {
    id: "node_frankfurt",
    name: "frankfurt",
    public_host: "198.51.100.22",
    hosts: [{ id: "host_frankfurt_1", host: "198.51.100.22", tag: "", selected: true }],
    api_base_url: "https://198.51.100.22:18080",
    status: "active",
    sing_box_version: "1.9.3",
    last_seen_at: iso(45_000),
    target_version: "v12",
    current_version: "v11",
    apply_status: "pending",
    latest_heartbeat: iso(45_000),
    agent_version: "0.4.1",
    capabilities: [...agentCapabilities],
    has_active_token: true,
    deleted_at: ""
  },
  {
    id: "node_singapore",
    name: "singapore",
    public_host: "192.0.2.31",
    hosts: [{ id: "host_singapore_1", host: "192.0.2.31", tag: "", selected: true }],
    api_base_url: "https://192.0.2.31:18080",
    status: "degraded",
    sing_box_version: "1.9.1",
    last_seen_at: iso(3 * HOUR),
    target_version: "v12",
    current_version: "v10",
    apply_status: "failed",
    apply_error: "sing-box check failed: timeout dialing reality handshake",
    latest_heartbeat: iso(3 * HOUR),
    agent_version: "0.3.9",
    capabilities: [...agentCapabilities],
    has_active_token: true,
    deleted_at: ""
  },
  {
    // Paused (disabled but token intact) — the row menu offers Enable.
    id: "node_osaka",
    name: "osaka",
    public_host: "203.0.113.55",
    hosts: [{ id: "host_osaka_1", host: "203.0.113.55", tag: "", selected: true }],
    api_base_url: "https://203.0.113.55:18080",
    status: "disabled",
    sing_box_version: "1.9.3",
    last_seen_at: iso(2 * HOUR),
    latest_heartbeat: iso(2 * HOUR),
    agent_version: "0.4.1",
    capabilities: [...agentCapabilities],
    has_active_token: true,
    deleted_at: ""
  },
  {
    // Decommissioned (disabled, tokens revoked) — menu shows re-enroll, not Enable.
    // An agent this old predates the operations protocol, so it reports no
    // capabilities and the row menu must not offer managed updates.
    id: "node_berlin",
    name: "berlin",
    public_host: "198.51.100.77",
    hosts: [{ id: "host_berlin_1", host: "198.51.100.77", tag: "", selected: true }],
    api_base_url: "",
    status: "disabled",
    sing_box_version: "1.9.0",
    last_seen_at: iso(5 * DAY),
    agent_version: "0.3.9",
    has_active_token: false,
    deleted_at: ""
  }
];

const users: AdminUser[] = [
  {
    id: "user_alice",
    name: "alice",
    display_name: "Alice Zhang",
    status: "active",
    global_quota_bytes: 500 * GiB,
    expire_at: iso(-30 * DAY),
    proxy_count: 3,
    deleted_at: ""
  },
  {
    id: "user_bob",
    name: "bob",
    display_name: "Bob Lee",
    status: "active",
    global_quota_bytes: 200 * GiB,
    expire_at: iso(-7 * DAY),
    proxy_count: 2,
    deleted_at: ""
  },
  {
    id: "user_carol",
    name: "carol",
    display_name: "Carol Wu",
    status: "disabled",
    global_quota_bytes: 100 * GiB,
    expire_at: iso(2 * DAY),
    proxy_count: 1,
    deleted_at: ""
  },
  // Enough of a tail that the paged inventory needs more than one page, with
  // every derived status represented: quota exhaustion and expiry are states the
  // stored column does not carry, so the page cannot be exercised without them.
  ...Array.from({ length: 21 }, (_, index): AdminUser => {
    const name = `user${String(index + 4).padStart(2, "0")}`;
    const expired = index % 5 === 2;
    const disabled = index % 7 === 3;
    const deleted = index === 19;
    return {
      id: `user_${name}`,
      name,
      display_name: `Team Member ${index + 4}`,
      status: disabled || deleted ? "disabled" : "active",
      global_quota_bytes: index % 4 === 0 ? 0 : (25 + index * 15) * GiB,
      expire_at: expired ? iso((index + 1) * DAY) : iso(-(index + 10) * DAY),
      proxy_count: index % 4,
      deleted_at: deleted ? iso(3 * DAY) : ""
    };
  })
];

const traffic: TrafficRow[] = [
  { user_name: "alice", direction: "uplink", raw_bytes: 42 * GiB, billable_bytes: 42 * GiB },
  { user_name: "alice", direction: "downlink", raw_bytes: 180 * GiB, billable_bytes: 180 * GiB },
  { user_name: "bob", direction: "uplink", raw_bytes: 11 * GiB, billable_bytes: 16 * GiB },
  { user_name: "bob", direction: "downlink", raw_bytes: 70 * GiB, billable_bytes: 105 * GiB },
  { user_name: "carol", direction: "uplink", raw_bytes: 2 * GiB, billable_bytes: 2 * GiB },
  { user_name: "carol", direction: "downlink", raw_bytes: 9 * GiB, billable_bytes: 9 * GiB },
  // Every fourth generated user has burned their whole quota, so the derived
  // "quota_exceeded" status has rows to match.
  ...users.slice(3).flatMap((user, index): TrafficRow[] => {
    const quota = user.global_quota_bytes;
    const billable = quota > 0 && index % 4 === 1 ? quota + 3 * GiB : (4 + index * 2) * GiB;
    return [
      { user_name: user.name, direction: "uplink", raw_bytes: Math.round(billable * 0.2), billable_bytes: Math.round(billable * 0.2) },
      { user_name: user.name, direction: "downlink", raw_bytes: Math.round(billable * 0.8), billable_bytes: Math.round(billable * 0.8) }
    ];
  })
];

const systemLogTemplates = [
  {
    node: "tokyo",
    service: "sing-box",
    level: "info",
    message: "inbound/vless[reality-in]: tcp connection from 100.64.2.5:51234"
  },
  {
    node: "frankfurt",
    service: "boxfleet-agent",
    level: "warn",
    message: "config apply pending: waiting for next pull cycle"
  },
  {
    node: "singapore",
    service: "boxfleet-agent",
    level: "error",
    message: "heartbeat failed: dial tcp 192.0.2.31:18080: i/o timeout"
  },
  {
    node: "tokyo",
    service: "systemd",
    level: "info",
    message: "Started sing-box.service - sing-box proxy service."
  },
  {
    node: "frankfurt",
    service: "sing-box",
    level: "debug",
    message: "router: matched outbound direct for tcp 172.66.40.248:443"
  },
  {
    node: "singapore",
    service: "sing-box",
    level: "warn",
    message: "reality handshake retry after upstream timeout"
  },
  {
    node: "tokyo",
    service: "boxfleet-agent",
    level: "info",
    message: "reported heartbeat with config version v12"
  },
  {
    node: "frankfurt",
    service: "systemd",
    level: "error",
    message: "sing-box.service: Main process exited, code=exited, status=1/FAILURE"
  }
] satisfies Array<Pick<SystemLog, "node" | "service" | "level" | "message">>;

const systemLogs: SystemLog[] = Array.from({ length: 36 }, (_, index) => {
  const template = systemLogTemplates[index % systemLogTemplates.length];
  const observed = (index + 1) * (index % 6 === 0 ? 2 * MIN : 7 * MIN);
  return {
    ...template,
    observed_at: iso(observed),
    ingested_at: iso(Math.max(30_000, observed - 45_000))
  };
});

const overview: Overview = {
  nodes,
  users,
  traffic,
  system_logs: systemLogs,
  system_log_note: "Showing the 100 most recent journald lines scraped from sing-box.",
  release: {
    repo: "haoxin/boxfleet",
    boxfleet_version: "0.4.1",
    agent_version: "0.4.1",
    sing_box_version: "1.9.3"
  }
};

const makeProxy = (over: Partial<AdminProxy> & Pick<AdminProxy, "id" | "node_name" | "name" | "listen_port">): AdminProxy => ({
  protocol: "vless",
  listen: "::",
  transport: "tcp",
  enabled: true,
  traffic_multiplier: 1,
  direct_publish: true,
  short_id: "a1b2c3d4",
  settings_json: JSON.stringify({ flow: "xtls-rprx-vision", short_id: "a1b2c3d4" }),
  inbound_rules_json: "[]",
  outbound_rules_json: "[]",
  route_rules_json: "[]",
  created_at: iso(30 * DAY),
  updated_at: iso(2 * DAY),
  deleted_at: "",
  ...over
});

const proxies: AdminProxy[] = [
  makeProxy({ id: "px_tokyo_1", node_name: "tokyo", name: "tokyo-reality", listen_port: 443 }),
  makeProxy({ id: "px_tokyo_2", node_name: "tokyo", name: "tokyo-reality-alt", listen_port: 8443, traffic_multiplier: 2 }),
  makeProxy({ id: "px_frankfurt_1", node_name: "frankfurt", name: "fra-reality", listen_port: 443 }),
  makeProxy({ id: "px_singapore_1", node_name: "singapore", name: "sg-reality", listen_port: 443, enabled: false })
];

const paths: AdminPath[] = proxies.flatMap((proxy) => {
  const node = nodes.find((item) => item.name === proxy.node_name);
  const hosts = (node?.hosts ?? [{ id: `host_${node?.id ?? proxy.node_name}`, host: node?.public_host ?? "", tag: "", selected: true }]).filter((host) => host.selected);
  return hosts.map((host, index) => ({
    id: `path_${proxy.id}_${index}`,
    name: host.tag || "direct",
    display_name: host.tag ? `${proxy.name}-${host.tag}` : index > 0 ? `${proxy.name}-${host.host}` : proxy.name,
    endpoint_id: `ep_${proxy.id}_${index}`,
    proxy_id: proxy.id,
    proxy_name: proxy.name,
    node_name: proxy.node_name,
    host_id: host.id || `host_${proxy.node_name}_${index}`,
    host: host.host,
    host_tag: host.tag,
    dialer_path_id: "",
    enabled: proxy.enabled,
    visibility: "selectable" as const,
    managed: true,
    sort_order: index,
    created_at: proxy.created_at,
    updated_at: proxy.updated_at
  }));
});

const userPathAccess = new Map<string, AdminPathAccess[]>();
function pathAccessFor(userName: string): AdminPathAccess[] {
  if (!userPathAccess.has(userName)) {
    userPathAccess.set(
      userName,
      paths.slice(0, userName === "alice" ? 3 : userName === "bob" ? 2 : 1).map((path) => ({
        id: `pacc_${userName}_${path.id}`,
        path_id: path.id,
        proxy_user_id: `user_${userName}`,
        enabled: true,
        deleted_at: "",
        created_at: iso(20 * DAY),
        updated_at: iso(DAY)
      }))
    );
  }
  return userPathAccess.get(userName) as AdminPathAccess[];
}

const proxyAccessFor = (userName: string): AdminProxyCredential[] =>
  proxies
    .filter((p) => p.enabled)
    .slice(0, userName === "alice" ? 3 : userName === "bob" ? 2 : 1)
    .map((p, i) => ({
      id: `acc_${userName}_${p.id}`,
      user_name: userName,
      node_name: p.node_name,
      proxy_name: p.name,
      protocol: p.protocol,
      listen: p.listen,
      listen_port: p.listen_port,
      transport: p.transport,
      auth_name: `${userName}@${p.node_name}`,
      enabled: true,
      quota_bytes: (i + 1) * 50 * GiB,
      proxy_multiplier: p.traffic_multiplier,
      created_at: iso(20 * DAY),
      updated_at: iso(DAY),
      deleted_at: ""
    }));

// Mutable per-user access store, seeded lazily from proxyAccessFor so the
// issue/revoke flow is demoable in dev without a real backend.
const userAccess = new Map<string, AdminProxyCredential[]>();
function accessFor(userName: string): AdminProxyCredential[] {
  if (!userAccess.has(userName)) userAccess.set(userName, proxyAccessFor(userName));
  return userAccess.get(userName) as AdminProxyCredential[];
}

const connectionInfoFor = (userName: string): UserConnectionInfo => {
  const activeAccesses = new Set(
    accessFor(userName)
      .filter((access) => access.enabled)
      .map((access) => `${access.node_name}\u0000${access.proxy_name}`)
  );
  return {
    user: userName,
    nodes: nodes
      .filter((n) => n.status === "active")
      .map((n) => ({
        user: userName,
        node: n.name,
        proxies: proxies
          .filter(
            (p) =>
              p.node_name === n.name &&
              p.enabled &&
              activeAccesses.has(`${p.node_name}\u0000${p.name}`)
          )
          .flatMap((p) =>
            (n.hosts ?? [{ host: n.public_host, tag: "", selected: true }])
              .filter((host) => host.selected)
              .map((host) => ({
                name: host.tag ? `${p.name}-${host.tag}` : p.name,
                proxy_name: p.name,
                host_tag: host.tag,
                type: "vless",
                server: host.host,
                server_port: p.listen_port,
                uuid: `00000000-0000-4000-8000-${p.id.replace(/[^0-9a-f]/gi, "").padEnd(12, "0").slice(0, 12)}`,
                flow: "xtls-rprx-vision",
                server_name: "www.cloudflare.com",
                public_key: "0Rsht7y9rH2nMpdJ8m1l8oUuTPwQ9cKuVqz4kf3aXmE",
                short_id: p.short_id
              }))
          )
      }))
      .filter((node) => node.proxies.length > 0)
  };
};

const subscriptions = new Map<string, AdminSubscription>([
  [
    "alice",
    {
      active: true,
      url: "http://127.0.0.1:5173/sub/bfsub_mock_alice",
      provider_url: "http://127.0.0.1:5173/sub/bfsub_mock_alice",
      mihomo_url: "http://127.0.0.1:5173/sub/bfsub_mock_alice/mihomo.yaml",
      created_at: iso(14 * DAY),
      last_used_at: iso(10 * MIN)
    }
  ]
]);

function subscriptionFor(userName: string): AdminSubscription {
  return subscriptions.get(userName) ?? {
    active: false,
    url: "",
    created_at: "",
    last_used_at: ""
  };
}

function issueSubscription(userName: string): AdminSubscription {
  const providerURL = `http://127.0.0.1:5173/sub/bfsub_mock_${userName}_${Date.now()}`;
  const subscription: AdminSubscription = {
    active: true,
    url: providerURL,
    provider_url: providerURL,
    mihomo_url: `${providerURL}/mihomo.yaml`,
    created_at: new Date().toISOString(),
    last_used_at: ""
  };
  subscriptions.set(userName, subscription);
  return subscription;
}

function proxyProviderFor(userName: string): string {
  const profiles = connectionInfoFor(userName).nodes.flatMap((node) =>
    node.proxies.map((proxy) => ({ node: node.node, ...proxy }))
  );
  if (profiles.length === 0) return "proxies: []\n";
  return `proxies:\n${profiles
    .map(
      (proxy) => `  - name: ${JSON.stringify(proxy.name)}
    type: vless
    server: ${JSON.stringify(proxy.server)}
    port: ${proxy.server_port}
    uuid: ${JSON.stringify(proxy.uuid)}
    udp: true
    flow: ${JSON.stringify(proxy.flow)}
    network: tcp
    tls: true
    servername: ${JSON.stringify(proxy.server_name)}
    client-fingerprint: chrome
    packet-encoding: xudp
    reality-opts:
      public-key: ${JSON.stringify(proxy.public_key)}
      short-id: ${JSON.stringify(proxy.short_id)}
    encryption: ""`
    )
    .join("\n")}\n`;
}

const networkTargets = [
  "api.github.com",
  "youtube.com",
  "registry.npmjs.org",
  "x.com",
  "speed.cloudflare.com",
  "developer.apple.com",
  "cloudflare.com",
  "go.dev"
];

const networkActions = ["connect", "outbound_connect", "invalid_connection", "reject"] as const;

const networkEvents: NetworkEvent[] = Array.from({ length: 96 }, (_, i) => {
  const u = users[i % users.length];
  const n = nodes[i % nodes.length];
  const action = networkActions[i % networkActions.length];
  const target = networkTargets[i % networkTargets.length];
  return {
    node_name: n.name,
    user_name: u.name,
    auth_name: `${u.name}@${n.name}`,
    source_ip: `100.64.${i % 12}.${((i + 1) * 7) % 254}`,
    target_host: target,
    target_port: i % 8 === 0 ? 80 : 443,
    action,
    raw_message: `${action}: ${u.name}@${n.name} -> ${target}`,
    count: 1 + (i % 5),
    window_start: iso((i + 1) * 15 * MIN),
    window_end: iso(i * 15 * MIN),
    created_at: iso(i * 15 * MIN)
  };
});

// --- Telemetry series ------------------------------------------------------
// The real server owns bucketing and zero-fill, so the mock does too: handlers
// return one point per bucket across the whole requested window, including
// empty ones. Buckets are keyed on window_start, never created_at.

type SeriesWindow = { start: number; end: number; bucket: SeriesBucket; offsetMinutes: number };

function timeWindow(query: URLSearchParams): { start: number; end: number } {
  const parsedEnd = Date.parse(query.get("end") ?? "");
  const end = Number.isFinite(parsedEnd) ? parsedEnd : now;
  const parsedStart = Date.parse(query.get("start") ?? "");
  const start = Number.isFinite(parsedStart) ? parsedStart : end - DAY;
  return { start, end };
}

// The service and host breakdowns leave start/end optional and echo them back
// normalized, exactly as the server does.
function normalizedTime(raw: string | null): string {
  const parsed = Date.parse(raw ?? "");
  return Number.isFinite(parsed) ? new Date(parsed).toISOString() : "";
}

function seriesWindow(query: URLSearchParams): SeriesWindow {
  const { start, end } = timeWindow(query);
  const requested = query.get("bucket");
  const bucket: SeriesBucket =
    requested === "hour" || requested === "day" ? requested : end - start <= 48 * HOUR ? "hour" : "day";
  return { start, end, bucket, offsetMinutes: Number(query.get("offset_minutes") ?? 0) || 0 };
}

// Hour buckets are UTC; day buckets are local midnight expressed as the UTC
// instant, which is what offset_minutes shifts.
function bucketFloor(ms: number, window: Pick<SeriesWindow, "bucket" | "offsetMinutes">): number {
  if (window.bucket === "hour") return Math.floor(ms / HOUR) * HOUR;
  const shifted = ms + window.offsetMinutes * MIN;
  return Math.floor(shifted / DAY) * DAY - window.offsetMinutes * MIN;
}

// Go renders a whole-second time.Time without a fractional part; match it so
// fixtures and the real server are byte-comparable.
const bucketISO = (ms: number) => new Date(ms).toISOString().replace(/\.\d{3}Z$/, "Z");

function bucketStarts(window: SeriesWindow): number[] {
  const step = window.bucket === "hour" ? HOUR : DAY;
  const starts: number[] = [];
  for (let cursor = bucketFloor(window.start, window); cursor <= window.end; cursor += step) starts.push(cursor);
  return starts;
}

// FNV-1a so a given series/bucket pair always renders the same number and the
// dev UI stops flickering on every refetch.
function hashSeed(text: string): number {
  let hash = 2166136261;
  for (let index = 0; index < text.length; index += 1) {
    hash ^= text.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return (hash >>> 0) / 2 ** 32;
}

function seriesLimit(query: URLSearchParams, fallback: number, max: number): number {
  return Math.max(1, Math.min(Number(query.get("limit") ?? fallback) || fallback, max));
}

function trafficPoint(key: string, bucketStart: number, window: SeriesWindow): TrafficPoint {
  const seed = hashSeed(`${key}:${bucketStart}`);
  const hourOfDay = new Date(bucketStart).getUTCHours();
  const diurnal = 0.3 + 0.7 * (0.5 - 0.5 * Math.cos(((hourOfDay + 2) / 24) * 2 * Math.PI));
  const shape = window.bucket === "hour" ? diurnal : 14 * (0.6 + 0.4 * seed);
  const multiplier = hashSeed(key) > 0.6 ? 1.5 : 1;
  const uplink = Math.round((0.35 + seed) * shape * 160 * 1024 ** 2);
  const downlink = Math.round(uplink * (3.5 + seed * 2));
  return {
    bucket_start: bucketISO(bucketStart),
    uplink_raw_bytes: uplink,
    uplink_billable_bytes: Math.round(uplink * multiplier),
    downlink_raw_bytes: downlink,
    downlink_billable_bytes: Math.round(downlink * multiplier)
  };
}

function trafficSeriesResponse(query: URLSearchParams): TrafficSeriesResponse {
  const window = seriesWindow(query);
  const requestedGroup = query.get("group");
  const group: TrafficSeriesGroup =
    requestedGroup === "user" || requestedGroup === "node" ? requestedGroup : "total";
  const userName = (query.get("user") ?? "").trim();
  const nodeName = (query.get("node") ?? "").trim();
  const limit = seriesLimit(query, 25, 100);

  let keys: Array<{ key: string; label: string }>;
  if (group === "user") {
    keys = users
      .filter((user) => !userName || user.name === userName)
      .map((user) => ({ key: user.name, label: user.display_name || user.name }));
  } else if (group === "node") {
    keys = nodes
      .filter((node) => !nodeName || node.name === nodeName)
      .map((node) => ({ key: node.name, label: node.name }));
  } else {
    keys = [{ key: "total", label: "All traffic" }];
  }
  const truncated = keys.length > limit;
  const starts = bucketStarts(window);

  return {
    bucket: window.bucket,
    offset_minutes: window.offsetMinutes,
    start: new Date(window.start).toISOString(),
    end: new Date(window.end).toISOString(),
    group,
    series: keys.slice(0, limit).map(({ key, label }) => {
      const points = starts.map((bucketStart) => trafficPoint(key, bucketStart, window));
      return {
        key,
        label,
        points,
        totals: points.reduce(
          (sum, point) => ({
            uplink_raw_bytes: sum.uplink_raw_bytes + point.uplink_raw_bytes,
            uplink_billable_bytes: sum.uplink_billable_bytes + point.uplink_billable_bytes,
            downlink_raw_bytes: sum.downlink_raw_bytes + point.downlink_raw_bytes,
            downlink_billable_bytes: sum.downlink_billable_bytes + point.downlink_billable_bytes
          }),
          { uplink_raw_bytes: 0, uplink_billable_bytes: 0, downlink_raw_bytes: 0, downlink_billable_bytes: 0 }
        )
      };
    }),
    truncated
  };
}

// The table endpoint and every aggregation over it must apply identical
// predicates, otherwise the chart and the rows below it disagree.
function filterNetworkEvents(query: URLSearchParams): NetworkEvent[] {
  const search = (query.get("search") ?? "").trim().toLowerCase();
  const action = (query.get("action") ?? "").trim().toLowerCase();
  const nodeName = (query.get("node") ?? "").trim();
  const userName = (query.get("user") ?? "").trim();
  const start = Date.parse(query.get("start") ?? "");
  const end = Date.parse(query.get("end") ?? "");
  return networkEvents
    .filter((event) => !nodeName || event.node_name === nodeName)
    .filter((event) => !userName || event.user_name === userName)
    .filter((event) => !action || event.action.toLowerCase() === action)
    .filter((event) => !Number.isFinite(start) || Date.parse(event.window_end) >= start)
    .filter((event) => !Number.isFinite(end) || Date.parse(event.window_start) <= end)
    .filter((event) => {
      if (!search) return true;
      return [
        event.node_name,
        event.user_name,
        event.auth_name,
        event.source_ip,
        event.target_host,
        String(event.target_port),
        event.action,
        event.raw_message
      ].some((value) => value.toLowerCase().includes(search));
    });
}

function networkEventSeriesResponse(query: URLSearchParams): NetworkEventSeriesResponse {
  const window = seriesWindow(query);
  const requestedGroup = query.get("group");
  const group: NetworkEventSeriesGroup =
    requestedGroup === "action" || requestedGroup === "node" || requestedGroup === "user" ? requestedGroup : "total";
  const limit = seriesLimit(query, 25, 100);
  const events = filterNetworkEvents(query);
  const starts = bucketStarts(window);

  const buckets = new Map<string, Map<number, number>>();
  const actions = new Map<string, number>();
  for (const event of events) {
    actions.set(event.action, (actions.get(event.action) ?? 0) + event.count);
    const key =
      group === "action" ? event.action : group === "node" ? event.node_name : group === "user" ? event.user_name : "total";
    const bucketStart = bucketFloor(Date.parse(event.window_start), window);
    const counts = buckets.get(key) ?? new Map<number, number>();
    counts.set(bucketStart, (counts.get(bucketStart) ?? 0) + event.count);
    buckets.set(key, counts);
  }
  if (group === "total" && !buckets.has("total")) buckets.set("total", new Map());

  const series = [...buckets.entries()]
    .map(([key, counts]) => ({
      key,
      label: group === "total" ? "All events" : key,
      points: starts.map((bucketStart) => ({
        bucket_start: bucketISO(bucketStart),
        count: counts.get(bucketStart) ?? 0
      })),
      total: [...counts.values()].reduce((sum, count) => sum + count, 0)
    }))
    .sort((left, right) => right.total - left.total || compareText(left.key, right.key, 1));

  return {
    bucket: window.bucket,
    offset_minutes: window.offsetMinutes,
    start: new Date(window.start).toISOString(),
    end: new Date(window.end).toISOString(),
    group,
    series: series.slice(0, limit),
    actions: [...actions.entries()]
      .map(([action, count]) => ({ action, count }))
      .sort((left, right) => right.count - left.count || compareText(left.action, right.action, 1)),
    truncated: series.length > limit
  };
}

// --- Service classification ------------------------------------------------

const SERVICE_CATALOG_VERSION = "2026-07-01";

const mockServiceCatalog: Record<string, { service: string; label: string; category: string }> = {
  "github.com": { service: "github", label: "GitHub", category: "development" },
  "npmjs.org": { service: "npm", label: "npm Registry", category: "development" },
  "youtube.com": { service: "youtube", label: "YouTube", category: "media" },
  "x.com": { service: "x", label: "X", category: "social" },
  "cloudflare.com": { service: "cloudflare", label: "Cloudflare", category: "infrastructure" },
  "apple.com": { service: "apple", label: "Apple", category: "technology" }
};

const domainServiceOverrides = new Map<string, DomainServiceOverride>([
  [
    "internal.example.net",
    {
      suffix: "internal.example.net",
      service: "intranet",
      label: "Corporate intranet",
      category: "internal",
      created_at: iso(9 * DAY),
      updated_at: iso(9 * DAY)
    }
  ]
]);

type Classification = { service: string; label: string; category: string; source: ServiceClassificationSource };

function matchSuffix<T>(host: string, entries: Array<[string, T]>): T | undefined {
  let best: [string, T] | undefined;
  for (const entry of entries) {
    const [suffix] = entry;
    if (host !== suffix && !host.endsWith(`.${suffix}`)) continue;
    if (!best || suffix.length > best[0].length) best = entry;
  }
  return best?.[1];
}

function classifyHost(host: string): Classification {
  const lower = host.trim().toLowerCase();
  if (!lower) return { service: "unknown", label: "Unknown", category: "unknown", source: "unknown" };
  if (lower.includes(":") || /^\d{1,3}(\.\d{1,3}){3}$/.test(lower)) {
    return { service: lower, label: lower, category: "direct-ip", source: "ip" };
  }
  const override = matchSuffix(lower, [...domainServiceOverrides.entries()]);
  if (override) {
    return {
      service: override.service,
      label: override.label || override.service,
      category: override.category,
      source: "override"
    };
  }
  const catalog = matchSuffix(lower, Object.entries(mockServiceCatalog));
  if (catalog) return { ...catalog, source: "catalog" };
  const registrable = lower.split(".").slice(-2).join(".");
  return { service: registrable, label: registrable, category: "unclassified", source: "publicsuffix" };
}

function networkEventHosts(query: URLSearchParams): NetworkEventHost[] {
  const counts = new Map<string, { connections: number; last_seen: string }>();
  for (const event of filterNetworkEvents(query)) {
    // target_host keeps its original casing in SQLite, so aggregation lowercases.
    const host = event.target_host.toLowerCase();
    const entry = counts.get(host) ?? { connections: 0, last_seen: "" };
    entry.connections += event.count;
    if (event.window_end > entry.last_seen) entry.last_seen = event.window_end;
    counts.set(host, entry);
  }
  return [...counts.entries()]
    .map(([host, entry]) => {
      const classification = classifyHost(host);
      return {
        host,
        service: classification.service,
        service_label: classification.label,
        category: classification.category,
        source: classification.source,
        connections: entry.connections,
        last_seen: entry.last_seen
      };
    })
    .sort((left, right) => right.connections - left.connections || compareText(left.host, right.host, 1));
}

function networkEventHostsResponse(query: URLSearchParams): NetworkEventHostsResponse {
  const service = (query.get("service") ?? "").trim();
  const limit = seriesLimit(query, 50, 500);
  const offset = Math.max(0, Number(query.get("offset") ?? 0) || 0);
  const hosts = networkEventHosts(query).filter((host) => !service || host.service === service);
  return { hosts: hosts.slice(offset, offset + limit), total: hosts.length, limit, offset, truncated: false };
}

function networkEventServicesResponse(query: URLSearchParams): ServiceUsageResponse {
  const group: ServiceUsageGroup = query.get("group") === "category" ? "category" : "service";
  const limit = seriesLimit(query, 20, 100);
  const hosts = networkEventHosts(query);

  const grouped = new Map<string, ServiceUsageRow>();
  for (const host of hosts) {
    const key = group === "category" ? host.category : host.service;
    const row = grouped.get(key) ?? {
      key,
      label: group === "category" ? host.category : host.service_label,
      category: group === "category" ? "" : host.category,
      connections: 0,
      hosts: 0
    };
    row.connections += host.connections;
    row.hosts += 1;
    grouped.set(key, row);
  }
  const rows = [...grouped.values()].sort(
    (left, right) => right.connections - left.connections || compareText(left.key, right.key, 1)
  );
  const other = rows.slice(limit).reduce(
    (sum, row) => ({ ...sum, connections: sum.connections + row.connections, hosts: sum.hosts + row.hosts }),
    { key: "other", label: "Other", category: "", connections: 0, hosts: 0 } as ServiceUsageRow
  );

  return {
    start: normalizedTime(query.get("start")),
    end: normalizedTime(query.get("end")),
    group,
    rows: rows.slice(0, limit),
    other,
    total_connections: hosts.reduce((sum, host) => sum + host.connections, 0),
    total_hosts: hosts.length,
    truncated: false,
    catalog_version: SERVICE_CATALOG_VERSION
  };
}

// --- Connection telemetry --------------------------------------------------
// The sing-box 1.14 daemon stream. The fixture is deliberately a MIXED fleet:
// exactly one node streams and the other four do not, because that is the state
// the admin UI has to read correctly. Selecting a non-streaming node must show
// an explanation, not an empty table.

const connectionTelemetryNodes = [
  { node_name: "tokyo", listen_address: "127.0.0.1", listen_port: 9091 }
];

const connectionStreamNodes = new Set(connectionTelemetryNodes.map((node) => node.node_name));

const CONNECTION_BUCKET = 5 * MIN;

/** Buckets are five-minute grid points, matching model.ConnectionBucketInterval. */
const connectionBucketISO = (ms: number) => new Date(Math.floor(ms / CONNECTION_BUCKET) * CONNECTION_BUCKET).toISOString();

const connectionShapes = [
  { host: "www.youtube.com", user: "alice", auth: "vless-39090@alice", protocol: "tls", weight: 48 },
  { host: "i.ytimg.com", user: "alice", auth: "vless-39090@alice", protocol: "tls", weight: 9 },
  { host: "github.com", user: "bob", auth: "vless-39090@bob", protocol: "tls", weight: 6 },
  { host: "registry.npmjs.org", user: "bob", auth: "vless-39090@bob", protocol: "tls", weight: 3 },
  // Single-user Shadowsocks never populates Connection.user, so this row is
  // stored against no user at all. Its bytes still count; dropping them would
  // understate every per-host total.
  { host: "speed.cloudflare.com", user: "", auth: "", protocol: "tcp", weight: 14 }
];

const connectionEvents: ConnectionEvent[] = Array.from({ length: 240 }, (_, i) => {
  const shape = connectionShapes[i % connectionShapes.length];
  const bucketMs = now - (i + 1) * 12 * MIN;
  const seed = hashSeed(`${shape.host}:${bucketMs}`);
  const opened = 1 + Math.round(seed * 6);
  const uplink = Math.round((0.4 + seed) * shape.weight * 1024 ** 2);
  return {
    node_name: "tokyo",
    user_name: shape.user,
    auth_name: shape.auth,
    source_ip: `100.64.${i % 12}.${((i + 1) * 7) % 254}`,
    target_host: shape.host,
    target_port: 443,
    domain: "",
    network: "tcp",
    ip_version: 4,
    protocol: shape.protocol,
    inbound: shape.auth ? "vless-39090" : "ss-8388",
    inbound_type: shape.auth ? "vless" : "shadowsocks",
    rule: "final",
    outbound: "direct",
    outbound_type: "direct",
    chain: `${shape.auth ? "vless-39090" : "ss-8388"}>direct`,
    connections_opened: opened,
    connections_closed: opened,
    uplink_bytes: uplink,
    downlink_bytes: Math.round(uplink * (2.5 + seed * 3)),
    duration_ms_total: opened * Math.round(4000 + seed * 90_000),
    bucket_start: connectionBucketISO(bucketMs),
    window_start: connectionBucketISO(bucketMs),
    window_end: new Date(bucketMs + 4 * MIN).toISOString()
  };
});

function filterConnectionEvents(query: URLSearchParams): ConnectionEvent[] {
  const nodeName = (query.get("node") ?? "").trim();
  const userName = (query.get("user") ?? "").trim();
  const host = (query.get("host") ?? "").trim().toLowerCase();
  const start = Date.parse(query.get("start") ?? "");
  const end = Date.parse(query.get("end") ?? "");
  // A node that does not stream has no connection events at all — that is the
  // whole point of the opt-in, so the mock enforces it rather than pretending.
  if (nodeName && !connectionStreamNodes.has(nodeName)) return [];
  return connectionEvents
    .filter((event) => !userName || event.user_name === userName)
    .filter((event) => !host || event.target_host === host)
    .filter((event) => !Number.isFinite(start) || Date.parse(event.bucket_start) >= start)
    .filter((event) => !Number.isFinite(end) || Date.parse(event.bucket_start) <= end);
}

function emptyConnectionVolume(): ConnectionVolume {
  return {
    connections_opened: 0,
    connections_closed: 0,
    uplink_bytes: 0,
    downlink_bytes: 0,
    total_bytes: 0,
    duration_ms_total: 0
  };
}

function addConnectionVolume(into: ConnectionVolume, event: ConnectionEvent): ConnectionVolume {
  into.connections_opened += event.connections_opened;
  into.connections_closed += event.connections_closed;
  into.uplink_bytes += event.uplink_bytes;
  into.downlink_bytes += event.downlink_bytes;
  into.total_bytes += event.uplink_bytes + event.downlink_bytes;
  into.duration_ms_total += event.duration_ms_total;
  return into;
}

// Coverage is derived from the same filtered rows so the loss figure the UI
// prints next to an estimate always belongs to the window on screen. Attributed
// bytes exclude the unattributed Shadowsocks rows, which is exactly the gap the
// real collector reports.
function connectionCoverage(events: ConnectionEvent[]): ConnectionCoverage {
  const observed = events.reduce((sum, event) => sum + event.connections_opened, 0);
  const attributed = events
    .filter((event) => event.auth_name !== "")
    .reduce((sum, event) => sum + event.connections_opened, 0);
  const bytesObserved = events.reduce((sum, event) => sum + event.uplink_bytes + event.downlink_bytes, 0);
  const bytesAttributed = events
    .filter((event) => event.auth_name !== "")
    .reduce((sum, event) => sum + event.uplink_bytes + event.downlink_bytes, 0);
  return {
    connections_observed: observed,
    connections_attributed: attributed,
    connections_unattributed: observed - attributed,
    connections_orphaned: Math.round(observed * 0.01),
    stream_resets: events.length > 0 ? 2 : 0,
    dropped_buckets: events.length > 120 ? 4 : 0,
    bytes_observed: bytesObserved,
    bytes_attributed: bytesAttributed,
    attribution_ratio: bytesObserved > 0 ? bytesAttributed / bytesObserved : 1,
    reports: Math.ceil(events.length / 12)
  };
}

function connectionSeriesResponse(query: URLSearchParams): ConnectionSeriesResponse {
  const window = seriesWindow(query);
  const events = filterConnectionEvents(query);
  const buckets = new Map<number, ConnectionVolume>();
  for (const event of events) {
    const key = bucketFloor(Date.parse(event.bucket_start), window);
    buckets.set(key, addConnectionVolume(buckets.get(key) ?? emptyConnectionVolume(), event));
  }
  const points: ConnectionPoint[] = bucketStarts(window).map((bucketStart) => ({
    bucket_start: bucketISO(bucketStart),
    ...(buckets.get(bucketStart) ?? emptyConnectionVolume())
  }));
  return {
    bucket: window.bucket,
    offset_minutes: window.offsetMinutes,
    start: new Date(window.start).toISOString(),
    end: new Date(window.end).toISOString(),
    points,
    totals: events.reduce(addConnectionVolume, emptyConnectionVolume()),
    coverage: connectionCoverage(events)
  };
}

function connectionHostsResponse(query: URLSearchParams): ConnectionHostsResponse {
  const sort: ConnectionHostSort = query.get("group") === "connections" ? "connections" : "bytes";
  const limit = seriesLimit(query, 20, 100);
  const events = filterConnectionEvents(query);
  const grouped = new Map<string, ConnectionVolume>();
  for (const event of events) {
    grouped.set(event.target_host, addConnectionVolume(grouped.get(event.target_host) ?? emptyConnectionVolume(), event));
  }
  const hosts: ConnectionHost[] = [...grouped.entries()]
    .map(([host, volume]) => ({ host, ...volume }))
    .sort((left, right) =>
      sort === "connections"
        ? right.connections_opened - left.connections_opened || compareText(left.host, right.host, 1)
        : right.total_bytes - left.total_bytes || compareText(left.host, right.host, 1)
    );
  return {
    sort,
    hosts: hosts.slice(0, limit),
    totals: events.reduce(addConnectionVolume, emptyConnectionVolume()),
    distinct_hosts: hosts.length,
    limit: Math.min(hosts.length, limit),
    truncated: hosts.length > limit,
    coverage: connectionCoverage(events)
  };
}

const basicMihomoYAML = `mixed-port: 7890
mode: rule
dns:
  enable: true
proxy-groups:
  - name: PROXY
    type: select
    proxies: [DIRECT]
rules:
  - MATCH,PROXY
`;
const mihomoTemplates: MihomoRewriteTemplate[] = [
  {
    id: "mhrt_basic",
    name: "BoxFleet Basic",
    description: "A ready-to-use Mihomo baseline with DNS, groups, and rules.",
    kind: "yaml",
    content: basicMihomoYAML,
    built_in: true,
    created_at: iso(30 * DAY),
    updated_at: iso(30 * DAY)
  }
];
const basicMihomoDocument: MihomoProfileDocument = { rewrites: [{
  id: "rw_basic",
  template_id: "mhrt_basic",
  name: "BoxFleet Basic",
  kind: "yaml",
  content: basicMihomoYAML,
  enabled: true
}] };
const mihomoProfiles: MihomoProfile[] = [
  {
    id: "mhp_alice_desktop",
    name: "Alice desktop",
    description: "Default desktop subscription.",
    proxy_user_id: "user_alice",
    proxy_user_name: "alice",
    document: basicMihomoDocument,
    created_at: iso(30 * DAY),
    updated_at: iso(DAY)
  }
];
const mihomoSubscriptions = new Map<string, MihomoProfileSubscription>();

function mihomoPreview(): MihomoPreview {
  const yaml = `mixed-port: 7890\nmode: rule\ndns:\n  enable: true\nproxies:\n  - name: tokyo-reality\n    type: vless\nproxy-groups:\n  - name: PROXY\n    type: select\n    include-all-proxies: true\nrules:\n  - GEOIP,CN,DIRECT\n  - MATCH,PROXY\n`;
  return { yaml, logs: [], diagnostics: [] };
}

const configChanges = {
  changed: [
    {
      node: "frankfurt",
      target_hash: "sha256:9f2c…a1",
      rendered_hash: "sha256:0b71…e4",
      target_version: "v12",
      target_config: '{\n  "log": { "level": "info" },\n  "inbounds": []\n}',
      rendered_config: '{\n  "log": { "level": "warn" },\n  "inbounds": [\n    { "type": "vless", "listen_port": 443 }\n  ]\n}'
    }
  ] as Array<{
    node: string;
    target_hash: string;
    rendered_hash: string;
    target_version: string;
    target_config: string;
    rendered_config: string;
  }>
};

// Dev helper: record that a node's rendered config now differs from what was
// published, so a write mutation lights up the global publish bar end-to-end.
function markNodeChanged(nodeName: string) {
  if (!nodeName || configChanges.changed.some((c) => c.node === nodeName)) return;
  configChanges.changed.push({
    node: nodeName,
    target_hash: "sha256:prev",
    rendered_hash: "sha256:next",
    target_version: "v12",
    target_config: '{\n  "inbounds": [\n    { "type": "vless", "listen_port": 443 }\n  ]\n}',
    rendered_config: '{\n  "inbounds": [\n    { "type": "vless", "listen_port": 443 },\n    { "type": "vless", "listen_port": 8443 }\n  ]\n}'
  });
}

function pageParams(query: URLSearchParams, fallback = 50) {
  const limit = Math.max(1, Math.min(Number(query.get("limit") ?? fallback) || fallback, 500));
  const offset = Math.max(0, Number(query.get("offset") ?? 0) || 0);
  return { limit, offset };
}

function sortDirection(query: URLSearchParams) {
  return query.get("direction") === "desc" ? -1 : 1;
}

function compareText(left: string | number | boolean | undefined, right: string | number | boolean | undefined, direction: number) {
  return String(left ?? "").localeCompare(String(right ?? ""), undefined, { numeric: true }) * direction;
}

function nodesPage(query: URLSearchParams): AdminNodesResponse {
  const search = (query.get("search") ?? "").trim().toLowerCase();
  const status = (query.get("status") ?? "").trim();
  const sort = query.get("sort") ?? "name";
  const direction = sortDirection(query);
  const { limit, offset } = pageParams(query);
  const deleted = query.get("deleted") === "true";
  const filtered = nodes
    .filter((node) => deleted ? Boolean(node.deleted_at) : !node.deleted_at)
    .filter((node) => !status || node.status === status)
    .filter((node) => {
      if (!search) return true;
      return [node.name, node.public_host, node.api_base_url, node.status, node.sing_box_version, node.agent_version]
        .some((value) => value?.toLowerCase().includes(search));
    })
    .sort((a, b) => {
      switch (sort) {
        case "status":
          return compareText(a.status, b.status, direction) || compareText(a.name, b.name, 1);
        case "public_host":
          return compareText(a.public_host, b.public_host, direction) || compareText(a.name, b.name, 1);
        case "last_seen_at":
          return compareText(a.latest_heartbeat || a.last_seen_at, b.latest_heartbeat || b.last_seen_at, direction) || compareText(a.name, b.name, 1);
        case "sing_box_version":
          return compareText(a.sing_box_version, b.sing_box_version, direction) || compareText(a.name, b.name, 1);
        default:
          return compareText(a.name, b.name, direction);
      }
    });
  return { nodes: filtered.slice(offset, offset + limit), total: filtered.length, limit, offset };
}

function proxiesPage(query: URLSearchParams): AdminProxiesResponse {
  const search = (query.get("search") ?? "").trim().toLowerCase();
  const enabled = (query.get("enabled") ?? "").trim();
  const nodeName = (query.get("node") ?? "").trim();
  const sort = query.get("sort") ?? "node_name";
  const direction = sortDirection(query);
  const { limit, offset } = pageParams(query);
  const deleted = query.get("deleted") === "true";
  const filtered = proxies
    .filter((proxy) => deleted ? Boolean(proxy.deleted_at) : !proxy.deleted_at)
    .filter((proxy) => !nodeName || proxy.node_name === nodeName)
    .filter((proxy) => {
      if (enabled === "true") return proxy.enabled;
      if (enabled === "false") return !proxy.enabled;
      return true;
    })
    .filter((proxy) => {
      if (!search) return true;
      return [proxy.name, proxy.node_name, proxy.protocol, proxy.listen, String(proxy.listen_port), proxy.transport]
        .some((value) => value?.toLowerCase().includes(search));
    })
    .sort((a, b) => {
      switch (sort) {
        case "name":
          return compareText(a.name, b.name, direction);
        case "protocol":
          return compareText(a.protocol, b.protocol, direction) || compareText(a.name, b.name, 1);
        case "listen_port":
          return compareText(a.listen_port, b.listen_port, direction) || compareText(a.name, b.name, 1);
        case "enabled":
          return compareText(a.enabled, b.enabled, direction) || compareText(a.name, b.name, 1);
        case "traffic_multiplier":
          return compareText(a.traffic_multiplier, b.traffic_multiplier, direction) || compareText(a.name, b.name, 1);
        case "updated_at":
          return compareText(a.updated_at, b.updated_at, direction) || compareText(a.name, b.name, 1);
        default:
          return compareText(a.node_name, b.node_name, direction) || compareText(a.listen_port, b.listen_port, 1) || compareText(a.name, b.name, 1);
      }
    });
  return { proxies: filtered.slice(offset, offset + limit), total: filtered.length, limit, offset };
}

// pageRequested mirrors the server's adminPageRequested: any one of these keys
// switches a list endpoint from its bare array to the page envelope.
function pageRequested(query: URLSearchParams): boolean {
  return ["limit", "offset", "search", "status", "enabled", "deleted", "node", "sort", "direction"]
    .some((key) => (query.get(key) ?? "").trim() !== "");
}

function userTraffic(name: string): TrafficVolume {
  const volume: TrafficVolume = {
    uplink_raw_bytes: 0,
    uplink_billable_bytes: 0,
    downlink_raw_bytes: 0,
    downlink_billable_bytes: 0
  };
  for (const row of traffic) {
    if (row.user_name !== name) continue;
    if (row.direction.includes("up")) {
      volume.uplink_raw_bytes += row.raw_bytes;
      volume.uplink_billable_bytes += row.billable_bytes;
    } else {
      volume.downlink_raw_bytes += row.raw_bytes;
      volume.downlink_billable_bytes += row.billable_bytes;
    }
  }
  return volume;
}

// Same precedence as userEffectiveStatusSQL on the server: deleted, disabled,
// over quota, expired, then active.
function effectiveStatus(user: AdminUser, volume: TrafficVolume): AdminUserEffectiveStatus {
  const billable = volume.uplink_billable_bytes + volume.downlink_billable_bytes;
  if (user.deleted_at) return "deleted";
  if (user.status === "disabled") return "disabled";
  if (user.status === "quota_exceeded" || (user.global_quota_bytes > 0 && billable >= user.global_quota_bytes)) {
    return "quota_exceeded";
  }
  if (user.status === "expired" || (user.expire_at && new Date(user.expire_at).getTime() <= now)) return "expired";
  return "active";
}

function usersPage(query: URLSearchParams): AdminUsersResponse {
  const search = (query.get("search") ?? "").trim().toLowerCase();
  const status = (query.get("status") ?? "").trim();
  const sort = query.get("sort") ?? "name";
  const direction = sortDirection(query);
  const { limit, offset } = pageParams(query);
  const deleted = query.get("deleted") === "true";
  const rows: AdminUserRow[] = users.map((user) => {
    const volume = userTraffic(user.name);
    return { ...user, traffic: volume, effective_status: effectiveStatus(user, volume) };
  });
  const billable = (row: AdminUserRow) => row.traffic.uplink_billable_bytes + row.traffic.downlink_billable_bytes;
  const filtered = rows
    .filter((row) => (deleted ? Boolean(row.deleted_at) : !row.deleted_at))
    .filter((row) => !status || row.effective_status === status)
    .filter((row) => {
      if (!search) return true;
      return [row.name, row.display_name, row.status, row.effective_status]
        .some((value) => value?.toLowerCase().includes(search));
    })
    .sort((a, b) => {
      switch (sort) {
        case "display_name":
          return compareText(a.display_name, b.display_name, direction) || compareText(a.name, b.name, 1);
        case "status":
          return compareText(a.effective_status, b.effective_status, direction) || compareText(a.name, b.name, 1);
        case "traffic":
          return compareText(billable(a), billable(b), direction) || compareText(a.name, b.name, 1);
        case "quota":
          return compareText(a.global_quota_bytes, b.global_quota_bytes, direction) || compareText(a.name, b.name, 1);
        case "proxy_count":
          return compareText(a.proxy_count, b.proxy_count, direction) || compareText(a.name, b.name, 1);
        case "expire_at":
          // A missing expiry sorts last, matching the server's sentinel.
          return compareText(a.expire_at || "9999-12-31T23:59:59Z", b.expire_at || "9999-12-31T23:59:59Z", direction)
            || compareText(a.name, b.name, 1);
        default:
          return compareText(a.name, b.name, direction);
      }
    });
  return { users: filtered.slice(offset, offset + limit), total: filtered.length, limit, offset };
}

// Journal levels are free text, so a bucket is a substring test and "info" is
// the residual bucket — the same rule the server applies.
function matchesLevel(level: string, bucket: string): boolean {
  const value = (level || "").toLowerCase();
  switch (bucket) {
    case "error":
      return value.includes("err") || value.includes("fatal");
    case "warn":
      return value.includes("warn");
    case "debug":
      return value.includes("debug") || value.includes("trace");
    case "info":
      return !["err", "fatal", "warn", "debug", "trace"].some((token) => value.includes(token));
    default:
      return true;
  }
}

function systemLogsResponse(query: URLSearchParams): SystemLogsResponse {
  const { limit, offset } = pageParams(query, 100);
  const nodeName = (query.get("node") ?? "").trim();
  const service = (query.get("service") ?? "").trim();
  const level = (query.get("level") ?? "").trim();
  const search = (query.get("search") ?? "").trim().toLowerCase();
  const sort = query.get("sort") ?? "observed_at";
  // Reading a journal is newest-first, so an unspecified direction means DESC.
  const direction = query.get("direction") === "asc" ? 1 : -1;
  const filtered = systemLogs
    .filter((log) => !nodeName || log.node === nodeName)
    .filter((log) => !service || log.service === service)
    .filter((log) => matchesLevel(log.level, level))
    .filter((log) => {
      if (!search) return true;
      return [log.node, log.service, log.level, log.message].some((value) => value.toLowerCase().includes(search));
    })
    .sort((a, b) => {
      switch (sort) {
        case "node":
          return compareText(a.node, b.node, direction) || compareText(b.observed_at, a.observed_at, 1);
        case "service":
          return compareText(a.service, b.service, direction) || compareText(b.observed_at, a.observed_at, 1);
        case "level":
          return compareText(a.level, b.level, direction) || compareText(b.observed_at, a.observed_at, 1);
        case "message":
          return compareText(a.message, b.message, direction) || compareText(b.observed_at, a.observed_at, 1);
        case "ingested_at":
          return compareText(a.ingested_at, b.ingested_at, direction) || compareText(b.observed_at, a.observed_at, 1);
        default:
          return compareText(a.observed_at, b.observed_at, direction);
      }
    });
  return {
    logs: filtered.slice(offset, offset + limit),
    // Options come from the whole fixture, never from the visible page.
    services: Array.from(new Set(systemLogs.map((log) => log.service))).sort(),
    total: filtered.length,
    limit,
    offset,
    // The server always sends an empty note; the fixture's own note belongs to
    // the overview payload only.
    note: ""
  };
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Handler = (ctx: { req: Connect.IncomingMessage; match: RegExpMatchArray | null; query: URLSearchParams; body?: any }) => unknown;
type Route = { method: string; pattern: RegExp; handler: Handler };

const routes: Route[] = [
  { method: "GET", pattern: /^\/api\/admin\/overview$/, handler: () => overview },
  {
    method: "GET",
    pattern: /^\/api\/admin\/release$/,
    handler: () => ({
      repo: "example/boxfleet",
      boxfleet_version: "0.4.1",
      agent_version: "0.4.1",
      sing_box_version: "1.9.3",
      updates_enabled: true
    })
  },
  { method: "GET", pattern: /^\/api\/admin\/node-update-campaigns\/current$/, handler: () => null },
  {
    method: "POST",
    pattern: /^\/api\/admin\/nodes\/([^/]+)\/updates$/,
    handler: ({ match }) => completedOperation(decodeURIComponent(match?.[1] ?? "")).operation
  },
  {
    method: "GET",
    pattern: /^\/api\/admin\/nodes\/([^/]+)\/operations\/([^/]+)$/,
    handler: ({ match }) => mockOperations.get(decodeURIComponent(match?.[2] ?? "")) ?? completedOperation(decodeURIComponent(match?.[1] ?? ""))
  },
  {
    method: "POST",
    pattern: /^\/api\/admin\/nodes\/([^/]+)\/operations\/([^/]+)\/cancel$/,
    handler: ({ match }) => mockOperations.get(decodeURIComponent(match?.[2] ?? ""))?.operation ?? completedOperation(decodeURIComponent(match?.[1] ?? "")).operation
  },
  {
    method: "POST",
    pattern: /^\/api\/admin\/node-updates\/bulk$/,
    handler: ({ body }) => completedCampaign(
      Array.isArray(body?.nodes) ? body.nodes : nodes.filter((node) => node.status === "active").map((node) => node.name)
    )
  },
  {
    method: "GET",
    pattern: /^\/api\/admin\/node-update-campaigns\/([^/]+)$/,
    handler: ({ match }) => mockCampaigns.get(decodeURIComponent(match?.[1] ?? ""))
  },
  {
    method: "POST",
    pattern: /^\/api\/admin\/node-update-campaigns\/([^/]+)\/(?:cancel|resume)$/,
    handler: ({ match }) => mockCampaigns.get(decodeURIComponent(match?.[1] ?? ""))
  },
  { method: "GET", pattern: /^\/api\/admin\/system-logs$/, handler: ({ query }) => systemLogsResponse(query) },
  { method: "GET", pattern: /^\/api\/admin\/config\/changes$/, handler: () => configChanges },
  {
    method: "POST",
    pattern: /^\/api\/admin\/config\/publish$/,
    handler: () => {
      const published = configChanges.changed.map((c) => ({ node: c.node, version: c.target_version, created: true }));
      // Advance the fixture so the apply poll converges and the bar turns green:
      // every tracked node now reports the target version as applied.
      for (const node of nodes) {
        if (node.status === "disabled" || node.status === "pending") continue;
        node.apply_status = "applied";
        if (node.target_version) node.current_version = node.target_version;
        delete node.apply_error;
      }
      configChanges.changed = [];
      return { published };
    }
  },
  { method: "GET", pattern: /^\/api\/admin\/proxies$/, handler: ({ query }) => query.has("limit") ? proxiesPage(query) : proxies.filter((proxy) => !proxy.deleted_at) },
  { method: "GET", pattern: /^\/api\/admin\/mihomo\/profiles$/, handler: () => mihomoProfiles },
  { method: "GET", pattern: /^\/api\/admin\/mihomo\/profiles\/([^/]+)$/, handler: ({ match }) => mihomoProfiles.find((item) => item.id === match?.[1]) ?? { ok: false } },
  { method: "GET", pattern: /^\/api\/admin\/mihomo\/rewrite-templates$/, handler: () => mihomoTemplates },
  {
    method: "POST",
    pattern: /^\/api\/admin\/mihomo\/rewrite-templates$/,
    handler: ({ body }): MihomoRewriteTemplate => {
      const template: MihomoRewriteTemplate = {
        id: `mhrt_${Date.now()}`, name: body?.name ?? "Rewrite", description: body?.description ?? "",
        kind: body?.kind ?? "yaml", content: body?.content ?? "", built_in: false,
        created_at: new Date().toISOString(), updated_at: new Date().toISOString()
      };
      mihomoTemplates.push(template);
      return template;
    }
  },
  {
    method: "PATCH",
    pattern: /^\/api\/admin\/mihomo\/rewrite-templates\/([^/]+)$/,
    handler: ({ match, body }) => {
      const template = mihomoTemplates.find((item) => item.id === match?.[1]);
      if (template && !template.built_in) {
        Object.assign(template, body, { updated_at: new Date().toISOString() });
        for (const profile of mihomoProfiles) {
          for (const rewrite of profile.document.rewrites) {
            if (rewrite.template_id === template.id) Object.assign(rewrite, { name: template.name, kind: template.kind, content: template.content });
          }
        }
      }
      return template ?? { ok: true };
    }
  },
  {
    method: "POST",
    pattern: /^\/api\/admin\/mihomo\/profiles$/,
    handler: ({ body }): MihomoProfile => {
      const profile: MihomoProfile = {
        id: `mhp_${Date.now()}`,
        name: body?.name ?? "New profile",
        description: body?.description ?? "",
        proxy_user_id: users.find((user) => user.name === body?.user)?.id ?? "",
        proxy_user_name: body?.user ?? "",
        document: body?.document ?? { rewrites: [] },
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString()
      };
      mihomoProfiles.push(profile);
      return profile;
    }
  },
  {
    method: "GET",
    pattern: /^\/api\/admin\/mihomo\/profiles\/([^/]+)\/subscription$/,
    handler: ({ match }) => mihomoSubscriptions.get(match?.[1] ?? "") ?? { active: false, url: "", created_at: "", last_used_at: "" }
  },
  {
    method: "POST",
    pattern: /^\/api\/admin\/mihomo\/profiles\/([^/]+)\/subscription(?:\/rotate)?$/,
    handler: ({ match }): MihomoProfileSubscription => {
      const subscription = {
        active: true,
        url: `http://127.0.0.1:5173/sub/bfsub_${Date.now()}/mihomo.yaml`,
        created_at: new Date().toISOString(),
        last_used_at: ""
      };
      mihomoSubscriptions.set(match?.[1] ?? "", subscription);
      return subscription;
    }
  },
  {
    method: "DELETE",
    pattern: /^\/api\/admin\/mihomo\/profiles\/([^/]+)\/subscription$/,
    handler: ({ match }) => {
      mihomoSubscriptions.delete(match?.[1] ?? "");
      return { active: false, url: "", created_at: "", last_used_at: "" };
    }
  },
  { method: "POST", pattern: /^\/api\/admin\/mihomo\/profiles\/([^/]+)\/preview$/, handler: () => mihomoPreview() },
  {
    method: "PATCH",
    pattern: /^\/api\/admin\/mihomo\/profiles\/([^/]+)$/,
    handler: ({ match, body }) => {
      const profile = mihomoProfiles.find((item) => item.id === match?.[1]);
      if (profile && body?.document) {
        profile.document = body.document;
        profile.updated_at = new Date().toISOString();
      }
      return profile ?? { ok: true };
    }
  },
  { method: "PUT", pattern: /^\/api\/admin\/users\/([^/]+)\/mihomo-profile$/, handler: () => mihomoProfiles[0] },
  {
    method: "POST",
    pattern: /^\/api\/admin\/nodes\/([^/]+)\/proxies$/,
    handler: ({ match, body }) => {
      const node = decodeURIComponent(match?.[1] ?? "");
      const proxy = makeProxy({
        id: `px_${now}_${proxies.length}`,
        node_name: node,
        name: body?.name ?? "new-proxy",
        listen_port: Number(body?.listen_port) || 443,
        enabled: body?.enabled ?? true,
        traffic_multiplier: Number(body?.traffic_multiplier) || 1,
        direct_publish: body?.direct_publish ?? true,
        settings_json:
          typeof body?.settings_json === "string"
            ? body.settings_json
            : JSON.stringify({ flow: "xtls-rprx-vision", short_id: "a1b2c3d4" })
      });
      proxies.push(proxy);
      markNodeChanged(node);
      return proxy;
    }
  },
  {
    method: "PATCH",
    pattern: /^\/api\/admin\/nodes\/([^/]+)\/proxies\/([^/]+)$/,
    handler: ({ match, body }) => {
      const node = decodeURIComponent(match?.[1] ?? "");
      const name = decodeURIComponent(match?.[2] ?? "");
      const proxy = proxies.find((p) => p.node_name === node && p.name === name);
      if (proxy && body) {
        if (typeof body.enabled === "boolean") proxy.enabled = body.enabled;
        if (typeof body.listen_port === "number") proxy.listen_port = body.listen_port;
        if (typeof body.traffic_multiplier === "number") proxy.traffic_multiplier = body.traffic_multiplier;
        if (typeof body.direct_publish === "boolean") proxy.direct_publish = body.direct_publish;
        if (typeof body.settings_json === "string") proxy.settings_json = body.settings_json;
        if (typeof body.short_id === "string") {
          proxy.short_id = body.short_id;
          try {
            const settings = JSON.parse(proxy.settings_json) as Record<string, unknown>;
            settings.short_id = body.short_id;
            proxy.settings_json = JSON.stringify(settings);
          } catch {
            proxy.settings_json = JSON.stringify({ short_id: body.short_id });
          }
        }
        if (typeof body.name === "string" && body.name.trim() && body.name.trim() !== proxy.name) {
          const oldName = proxy.name;
          proxy.name = body.name.trim();
          for (const accesses of userAccess.values()) {
            for (const access of accesses) {
              if (access.node_name === node && access.proxy_name === oldName) {
                access.proxy_name = proxy.name;
              }
            }
          }
        }
        proxy.updated_at = new Date().toISOString();
      }
      markNodeChanged(node);
      return proxy ?? { ok: true };
    }
  },
  {
    method: "DELETE",
    pattern: /^\/api\/admin\/nodes\/([^/]+)\/proxies\/([^/]+)$/,
    handler: ({ match }) => {
      const node = decodeURIComponent(match?.[1] ?? "");
      const name = decodeURIComponent(match?.[2] ?? "");
      const proxy = proxies.find((p) => p.node_name === node && p.name === name);
      if (proxy) {
        proxy.enabled = false;
        proxy.deleted_at = new Date().toISOString();
      }
      markNodeChanged(node);
      return proxy ?? { ok: true };
    }
  },
  {
    method: "POST",
    pattern: /^\/api\/admin\/nodes\/([^/]+)\/proxies\/([^/]+)\/restore$/,
    handler: ({ match }) => {
      const node = decodeURIComponent(match?.[1] ?? "");
      const name = decodeURIComponent(match?.[2] ?? "");
      const proxy = proxies.find((item) => item.node_name === node && item.name === name);
      if (proxy) proxy.deleted_at = "";
      return proxy ?? { ok: true };
    }
  },
  { method: "GET", pattern: /^\/api\/admin\/nodes$/, handler: ({ query }) => query.has("limit") ? nodesPage(query) : nodes.filter((node) => !node.deleted_at) },
  {
    method: "GET",
    pattern: /^\/api\/admin\/users$/,
    handler: ({ query }) => pageRequested(query)
      ? usersPage(query)
      : users.filter((user) => !user.deleted_at)
  },
  { method: "GET", pattern: /^\/api\/admin\/traffic\/users$/, handler: () => traffic },
  {
    method: "GET",
    pattern: /^\/api\/admin\/network-events$/,
    handler: ({ query }): NetworkEventsResponse => {
      const { limit, offset } = pageParams(query);
      const filtered = filterNetworkEvents(query);
      return { events: filtered.slice(offset, offset + limit), total: filtered.length, limit, offset };
    }
  },
  { method: "GET", pattern: /^\/api\/admin\/traffic\/series$/, handler: ({ query }) => trafficSeriesResponse(query) },
  { method: "GET", pattern: /^\/api\/admin\/network-events\/series$/, handler: ({ query }) => networkEventSeriesResponse(query) },
  { method: "GET", pattern: /^\/api\/admin\/network-events\/services$/, handler: ({ query }) => networkEventServicesResponse(query) },
  { method: "GET", pattern: /^\/api\/admin\/network-events\/hosts$/, handler: ({ query }) => networkEventHostsResponse(query) },
  {
    method: "GET",
    pattern: /^\/api\/admin\/connection-events$/,
    handler: ({ query }): ConnectionEventsResponse => {
      const { limit, offset } = pageParams(query);
      const filtered = filterConnectionEvents(query);
      return { events: filtered.slice(offset, offset + limit), total: filtered.length, limit, offset };
    }
  },
  { method: "GET", pattern: /^\/api\/admin\/connection-events\/series$/, handler: ({ query }) => connectionSeriesResponse(query) },
  { method: "GET", pattern: /^\/api\/admin\/connection-events\/hosts$/, handler: ({ query }) => connectionHostsResponse(query) },
  {
    method: "GET",
    pattern: /^\/api\/admin\/connection-events\/nodes$/,
    handler: (): ConnectionTelemetryNodesResponse => ({ nodes: connectionTelemetryNodes })
  },
  {
    method: "GET",
    pattern: /^\/api\/admin\/service-overrides$/,
    handler: () => [...domainServiceOverrides.values()].sort((left, right) => compareText(left.suffix, right.suffix, 1))
  },
  {
    method: "PUT",
    pattern: /^\/api\/admin\/service-overrides$/,
    handler: ({ body }): DomainServiceOverride => {
      const suffix = String(body?.suffix ?? "").trim().toLowerCase().replace(/^\.+/, "");
      const timestamp = new Date().toISOString();
      const override: DomainServiceOverride = {
        suffix,
        service: String(body?.service ?? ""),
        label: String(body?.label ?? ""),
        category: String(body?.category ?? ""),
        created_at: domainServiceOverrides.get(suffix)?.created_at ?? timestamp,
        updated_at: timestamp
      };
      domainServiceOverrides.set(suffix, override);
      return override;
    }
  },
  {
    method: "DELETE",
    pattern: /^\/api\/admin\/service-overrides\/([^/]+)$/,
    handler: ({ match }) => {
      domainServiceOverrides.delete(decodeURIComponent(match?.[1] ?? "").toLowerCase());
      return { ok: true };
    }
  },
  {
    method: "POST",
    pattern: /^\/api\/admin\/nodes\/bootstrap$/,
    handler: ({ body }): AdminNodeBootstrap => {
      const name = (body?.name as string) || `node-${nodes.length}`;
      const publicHost = (body?.public_host as string) || "";
      const node: AdminNode = {
        id: `node_${name}`,
        name,
        public_host: publicHost,
        hosts: publicHost
          ? [{ id: mockHostID(`node_${name}`, publicHost), host: publicHost, tag: "", selected: true }]
          : [],
        api_base_url: "",
        status: "pending",
        sing_box_version: "",
        last_seen_at: "",
        // Bootstrap issues the node's first token, so the row is a pending
        // enrollment rather than a decommissioned node.
        has_active_token: true,
        deleted_at: ""
      };
      nodes.push(node);
      return {
        node,
        bootstrap_string: `BFNODE:${name}:eyJhcGkiOiJodHRwczovLzIwMy4wLjExMy4xMCJ9:devtoken`,
        install_script_url: "http://127.0.0.1:18081/install/node.sh"
      };
    }
  },
  {
    method: "POST",
    pattern: /^\/api\/admin\/nodes\/([^/]+)\/reenroll$/,
    handler: ({ match }): AdminNodeBootstrap => {
      const name = decodeURIComponent(match?.[1] ?? "");
      const node = nodes.find((n) => n.name === name);
      if (node) {
        node.status = "pending";
        node.has_active_token = true;
      }
      return {
        node: node ?? nodes[0],
        bootstrap_string: `BFNODE:${name}:eyJhcGkiOiJodHRwczovLzIwMy4wLjExMy4xMCJ9:devtoken2`,
        install_script_url: "http://127.0.0.1:18081/install/node.sh"
      };
    }
  },
  {
    method: "PATCH",
    pattern: /^\/api\/admin\/nodes\/([^/]+)$/,
    handler: ({ match, body }) => {
      const node = nodes.find((n) => n.name === decodeURIComponent(match?.[1] ?? ""));
      if (node && body) {
        const oldName = node.name;
        if (Array.isArray(body.hosts)) {
          const hosts = (body.hosts as AdminNode["hosts"]) ?? [];
          if (hosts.length > 0) {
            // The server mints an id for every host row it stores; a host that
            // reaches Paths without one cannot be selected as an endpoint.
            node.hosts = hosts.map((host) => ({ ...host, id: host.id || mockHostID(node.id, host.host) }));
            node.public_host = hosts[0].host;
          }
        } else if (typeof body.public_host === "string") {
          node.public_host = body.public_host;
          node.hosts = [
            { id: mockHostID(node.id, body.public_host), host: body.public_host, tag: "", selected: true }
          ];
        }
        if (typeof body.api_base_url === "string") node.api_base_url = body.api_base_url;
        if (body.status === "active" || body.status === "disabled") node.status = body.status;
        if (typeof body.name === "string" && body.name.trim() && body.name.trim() !== oldName) {
          node.name = body.name.trim();
          for (const proxy of proxies) {
            if (proxy.node_name === oldName) proxy.node_name = node.name;
          }
          for (const accesses of userAccess.values()) {
            for (const access of accesses) {
              if (access.node_name === oldName) access.node_name = node.name;
            }
          }
        }
      }
      if (node) markNodeChanged(node.name);
      return node ?? { ok: true };
    }
  },
  {
    method: "DELETE",
    pattern: /^\/api\/admin\/nodes\/([^/]+)$/,
    handler: ({ match }) => {
      const name = decodeURIComponent(match?.[1] ?? "");
      const node = nodes.find((n) => n.name === name);
      if (node) {
        node.status = "disabled";
        node.has_active_token = false;
        node.deleted_at = new Date().toISOString();
      }
      markNodeChanged(name);
      return node ?? { ok: true };
    }
  },
  {
    method: "POST",
    pattern: /^\/api\/admin\/nodes\/([^/]+)\/restore$/,
    handler: ({ match }) => {
      const node = nodes.find((item) => item.name === decodeURIComponent(match?.[1] ?? ""));
      if (node) node.deleted_at = "";
      return node ?? { ok: true };
    }
  },
  {
    method: "POST",
    pattern: /^\/api\/admin\/users$/,
    handler: ({ body }) => {
      const user: AdminUser = {
        id: `user_${now}_${users.length}`,
        name: body?.name ?? "new-user",
        display_name: body?.display_name ?? "",
        status: "active",
        global_quota_bytes: Number(body?.global_quota_bytes) || 0,
        expire_at: typeof body?.expire_at === "string" ? body.expire_at : "",
        proxy_count: 0,
        deleted_at: ""
      };
      users.push(user);
      return user;
    }
  },
  {
    method: "PATCH",
    pattern: /^\/api\/admin\/users\/([^/]+)$/,
    handler: ({ match, body }) => {
      const name = decodeURIComponent(match?.[1] ?? "");
      const user = users.find((u) => u.name === name);
      if (user && body) {
        if (typeof body.display_name === "string") user.display_name = body.display_name;
        if (body.status === "active" || body.status === "disabled") user.status = body.status;
        if (typeof body.global_quota_bytes === "number") user.global_quota_bytes = body.global_quota_bytes;
        if (typeof body.expire_at === "string") user.expire_at = body.expire_at;
      }
      return user ?? { ok: true };
    }
  },
  {
    method: "GET",
    pattern: /^\/api\/admin\/paths$/,
    handler: () => paths
  },
  {
    method: "POST",
    pattern: /^\/api\/admin\/paths$/,
    handler: ({ body }) => {
      const proxy = proxies.find((item) => item.id === body?.proxy_id);
      const node = nodes.find((item) => item.name === proxy?.node_name);
      const host = node?.hosts?.find((item) => item.id === body?.host_id);
      const path: AdminPath = {
        id: `path_custom_${paths.length + 1}`,
        name: String(body?.name ?? "path"),
        display_name: String(body?.display_name ?? ""),
        endpoint_id: `ep_custom_${paths.length + 1}`,
        proxy_id: proxy?.id ?? String(body?.proxy_id ?? ""),
        proxy_name: proxy?.name ?? "",
        node_name: proxy?.node_name ?? "",
        host_id: String(body?.host_id ?? ""),
        host: host?.host ?? node?.public_host ?? "",
        host_tag: host?.tag ?? "",
        dialer_path_id: String(body?.dialer_path_id ?? ""),
        enabled: body?.enabled !== false,
        visibility: body?.visibility === "dependency" ? "dependency" : "selectable",
        managed: false,
        sort_order: Number(body?.sort_order ?? 0),
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString()
      };
      paths.push(path);
      return path;
    }
  },
  {
    method: "PATCH",
    pattern: /^\/api\/admin\/paths\/([^/]+)$/,
    handler: ({ match, body }) => {
      const path = paths.find((item) => item.id === decodeURIComponent(match?.[1] ?? ""));
      if (path) Object.assign(path, body, { updated_at: new Date().toISOString() });
      return path ?? { ok: true };
    }
  },
  {
    method: "DELETE",
    pattern: /^\/api\/admin\/paths\/([^/]+)$/,
    handler: ({ match }) => {
      const index = paths.findIndex((item) => item.id === decodeURIComponent(match?.[1] ?? ""));
      if (index >= 0) paths.splice(index, 1);
      return { ok: true };
    }
  },
  {
    method: "GET",
    pattern: /^\/api\/admin\/users\/([^/]+)\/paths$/,
    handler: ({ match }) => pathAccessFor(decodeURIComponent(match?.[1] ?? "alice"))
  },
  {
    method: "POST",
    pattern: /^\/api\/admin\/users\/([^/]+)\/paths$/,
    handler: ({ match, body }) => {
      const userName = decodeURIComponent(match?.[1] ?? "");
      const pathID = String(body?.path_id ?? "");
      const list = pathAccessFor(userName);
      const existing = list.find((access) => access.path_id === pathID);
      if (existing) return existing;
      const access: AdminPathAccess = {
        id: `pacc_${userName}_${pathID}`,
        path_id: pathID,
        proxy_user_id: `user_${userName}`,
        enabled: true,
        deleted_at: "",
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString()
      };
      list.push(access);
      return access;
    }
  },
  {
    method: "DELETE",
    pattern: /^\/api\/admin\/users\/([^/]+)\/paths\/([^/]+)$/,
    handler: ({ match }) => {
      const userName = decodeURIComponent(match?.[1] ?? "");
      const pathID = decodeURIComponent(match?.[2] ?? "");
      const list = pathAccessFor(userName);
      const index = list.findIndex((access) => access.path_id === pathID);
      if (index >= 0) list.splice(index, 1);
      return { ok: true };
    }
  },
  {
    method: "POST",
    pattern: /^\/api\/admin\/users\/([^/]+)\/proxies$/,
    handler: ({ match, body }) => {
      const name = decodeURIComponent(match?.[1] ?? "");
      const proxy = proxies.find((p) => p.node_name === body?.node_name && p.name === body?.proxy_name);
      const list = accessFor(name);
      if (proxy && !list.some((a) => a.node_name === proxy.node_name && a.proxy_name === proxy.name)) {
        const access: AdminProxyCredential = {
          id: `acc_${name}_${proxy.id}`,
          user_name: name,
          node_name: proxy.node_name,
          proxy_name: proxy.name,
          protocol: proxy.protocol,
          listen: proxy.listen,
          listen_port: proxy.listen_port,
          transport: proxy.transport,
          auth_name: `${name}@${proxy.node_name}`,
          enabled: true,
          quota_bytes: 0,
          proxy_multiplier: proxy.traffic_multiplier,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
          deleted_at: ""
        };
        list.push(access);
        const user = users.find((u) => u.name === name);
        if (user) user.proxy_count = list.length;
        markNodeChanged(proxy.node_name);
        return access;
      }
      return { ok: true };
    }
  },
  {
    method: "DELETE",
    pattern: /^\/api\/admin\/users\/([^/]+)$/,
    handler: ({ match }) => {
      const user = users.find((item) => item.name === decodeURIComponent(match?.[1] ?? ""));
      if (user) {
        user.status = "disabled";
        user.deleted_at = new Date().toISOString();
      }
      return user ?? { ok: true };
    }
  },
  {
    method: "POST",
    pattern: /^\/api\/admin\/users\/([^/]+)\/restore$/,
    handler: ({ match }) => {
      const user = users.find((item) => item.name === decodeURIComponent(match?.[1] ?? ""));
      if (user) user.deleted_at = "";
      return user ?? { ok: true };
    }
  },
  {
    method: "DELETE",
    pattern: /^\/api\/admin\/users\/([^/]+)\/proxies\/([^/]+)\/([^/]+)$/,
    handler: ({ match }) => {
      const name = decodeURIComponent(match?.[1] ?? "");
      const node = decodeURIComponent(match?.[2] ?? "");
      const proxyName = decodeURIComponent(match?.[3] ?? "");
      const list = accessFor(name);
      const idx = list.findIndex((a) => a.node_name === node && a.proxy_name === proxyName);
      if (idx >= 0) list.splice(idx, 1);
      const user = users.find((u) => u.name === name);
      if (user) user.proxy_count = list.length;
      markNodeChanged(node);
      return { ok: true };
    }
  },
  { method: "GET", pattern: /^\/api\/admin\/users\/([^/]+)\/proxies$/, handler: ({ match }) => accessFor(decodeURIComponent(match?.[1] ?? "alice")) },
  { method: "GET", pattern: /^\/api\/admin\/users\/([^/]+)\/connection-info$/, handler: ({ match }) => connectionInfoFor(decodeURIComponent(match?.[1] ?? "alice")) },
  { method: "GET", pattern: /^\/api\/admin\/users\/([^/]+)\/proxy-provider$/, handler: ({ match }) => proxyProviderFor(decodeURIComponent(match?.[1] ?? "alice")) },
  { method: "GET", pattern: /^\/api\/admin\/users\/([^/]+)\/subscription$/, handler: ({ match }) => subscriptionFor(decodeURIComponent(match?.[1] ?? "alice")) },
  { method: "POST", pattern: /^\/api\/admin\/users\/([^/]+)\/subscription$/, handler: ({ match }) => issueSubscription(decodeURIComponent(match?.[1] ?? "alice")) },
  { method: "POST", pattern: /^\/api\/admin\/users\/([^/]+)\/subscription\/rotate$/, handler: ({ match }) => issueSubscription(decodeURIComponent(match?.[1] ?? "alice")) },
  {
    method: "DELETE",
    pattern: /^\/api\/admin\/users\/([^/]+)\/subscription$/,
    handler: ({ match }) => {
      const name = decodeURIComponent(match?.[1] ?? "alice");
      subscriptions.delete(name);
      return subscriptionFor(name);
    }
  }
];

function jsonResponse(res: import("node:http").ServerResponse, status: number, body: unknown) {
  res.statusCode = status;
  res.setHeader("Content-Type", "application/json");
  res.end(JSON.stringify(body));
}

// Echo fallback for write operations (POST/PUT/PATCH/DELETE) so optimistic UI
// flows resolve instead of throwing. Returns a generic ok-shaped object.
function writeFallback(method: string): unknown {
  if (method === "DELETE") return { ok: true };
  return { id: `mock_${Date.now()}`, ok: true };
}

export function adminMockPlugin(): Plugin {
  return {
    name: "boxfleet-admin-mock",
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const rawUrl = req.url ?? "";
        if (!rawUrl.startsWith("/api/admin")) {
          next();
          return;
        }
        const [pathname, search = ""] = rawUrl.split("?");
        const method = (req.method ?? "GET").toUpperCase();
        const query = new URLSearchParams(search);

        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const dispatch = (body?: any) => {
          for (const route of routes) {
            if (route.method !== method) continue;
            const match = pathname.match(route.pattern);
            if (!match) continue;
            jsonResponse(res, 200, route.handler({ req, match, query, body }));
            return;
          }
          if (method !== "GET") {
            jsonResponse(res, 200, writeFallback(method));
            return;
          }
          jsonResponse(res, 404, { error: `mock: no fixture for ${method} ${pathname}` });
        };

        if (method === "GET" || method === "DELETE") {
          dispatch();
          return;
        }

        // Buffer the JSON body for POST/PATCH/PUT so handlers can reflect edits.
        let raw = "";
        req.on("data", (chunk) => {
          raw += chunk;
        });
        req.on("end", () => {
          let parsed: unknown;
          try {
            parsed = raw ? JSON.parse(raw) : undefined;
          } catch {
            parsed = undefined;
          }
          dispatch(parsed);
        });
      });
    }
  };
}
