import type { Connect, Plugin } from "vite";

import type {
  AdminNode,
  AdminNodeBootstrap,
  AdminNodesResponse,
  AdminProxy,
  AdminProxyAccess,
  AdminProxiesResponse,
  AdminSettings,
  AdminSubscription,
  AdminUser,
  MihomoPreview,
  MihomoProfile,
  MihomoProfileDocument,
  MihomoProfileRevision,
  MihomoProfileSubscription,
  MihomoRewriteTemplate,
  NetworkEvent,
  NetworkEventsResponse,
  Overview,
  SystemLog,
  SystemLogsResponse,
  TrafficRow,
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

const nodes: AdminNode[] = [
  {
    id: "node_tokyo",
    name: "tokyo",
    public_host: "tokyo.example.net",
    hosts: [
      { host: "tokyo.example.net", tag: "", selected: true },
      { host: "203.0.113.10", tag: "ipv4", selected: true },
      { host: "2606:4700::6810:84e5", tag: "ipv6", selected: false }
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
    has_active_token: true,
    deleted_at: ""
  },
  {
    id: "node_frankfurt",
    name: "frankfurt",
    public_host: "198.51.100.22",
    api_base_url: "https://198.51.100.22:18080",
    status: "active",
    sing_box_version: "1.9.3",
    last_seen_at: iso(45_000),
    target_version: "v12",
    current_version: "v11",
    apply_status: "pending",
    latest_heartbeat: iso(45_000),
    agent_version: "0.4.1",
    has_active_token: true,
    deleted_at: ""
  },
  {
    id: "node_singapore",
    name: "singapore",
    public_host: "192.0.2.31",
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
    has_active_token: true,
    deleted_at: ""
  },
  {
    // Paused (disabled but token intact) — the row menu offers Enable.
    id: "node_osaka",
    name: "osaka",
    public_host: "203.0.113.55",
    api_base_url: "https://203.0.113.55:18080",
    status: "disabled",
    sing_box_version: "1.9.3",
    last_seen_at: iso(2 * HOUR),
    latest_heartbeat: iso(2 * HOUR),
    agent_version: "0.4.1",
    has_active_token: true,
    deleted_at: ""
  },
  {
    // Decommissioned (disabled, tokens revoked) — menu shows re-enroll, not Enable.
    id: "node_berlin",
    name: "berlin",
    public_host: "198.51.100.77",
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
  }
];

const traffic: TrafficRow[] = [
  { user_name: "alice", direction: "uplink", raw_bytes: 42 * GiB, billable_bytes: 42 * GiB },
  { user_name: "alice", direction: "downlink", raw_bytes: 180 * GiB, billable_bytes: 180 * GiB },
  { user_name: "bob", direction: "uplink", raw_bytes: 11 * GiB, billable_bytes: 16 * GiB },
  { user_name: "bob", direction: "downlink", raw_bytes: 70 * GiB, billable_bytes: 105 * GiB },
  { user_name: "carol", direction: "uplink", raw_bytes: 2 * GiB, billable_bytes: 2 * GiB },
  { user_name: "carol", direction: "downlink", raw_bytes: 9 * GiB, billable_bytes: 9 * GiB }
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
    sing_box_version: "1.9.3"
  }
};

const makeProxy = (over: Partial<AdminProxy> & Pick<AdminProxy, "id" | "node_name" | "name" | "listen_port">): AdminProxy => ({
  protocol: "vless",
  listen: "::",
  transport: "tcp",
  enabled: true,
  traffic_multiplier: 1,
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

const proxyAccessFor = (userName: string): AdminProxyAccess[] =>
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
const userAccess = new Map<string, AdminProxyAccess[]>();
function accessFor(userName: string): AdminProxyAccess[] {
  if (!userAccess.has(userName)) userAccess.set(userName, proxyAccessFor(userName));
  return userAccess.get(userName) as AdminProxyAccess[];
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

const settings: AdminSettings = { network_event_retention_days: 90 };

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
    draft: basicMihomoDocument,
    published_revision_id: "mhpr_alice_1",
    published_version: 1,
    published: basicMihomoDocument,
    created_at: iso(30 * DAY),
    updated_at: iso(DAY)
  }
];
const mihomoRevisions = new Map<string, MihomoProfileRevision[]>([
  [
    "mhp_alice_desktop",
    [{ id: "mhpr_alice_1", profile_id: "mhp_alice_desktop", version: 1, document: basicMihomoDocument, created_at: iso(30 * DAY) }]
  ]
]);
const mihomoSubscriptions = new Map<string, MihomoProfileSubscription>();

function mihomoPreview(): MihomoPreview {
  const yaml = `mixed-port: 7890\nmode: rule\ndns:\n  enable: true\nproxies:\n  - name: tokyo-reality\n    type: vless\nproxy-groups:\n  - name: PROXY\n    type: select\n    include-all-proxies: true\nrules:\n  - GEOIP,CN,DIRECT\n  - MATCH,PROXY\n`;
  return { yaml, published_yaml: yaml, logs: [], diagnostics: [] };
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

function pageParams(query: URLSearchParams) {
  const limit = Math.max(1, Math.min(Number(query.get("limit") ?? 50) || 50, 500));
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

function systemLogsResponse(query: URLSearchParams): SystemLogsResponse {
  const { limit } = pageParams(query);
  const nodeName = (query.get("node") ?? "").trim();
  const logs = systemLogs.filter((log) => !nodeName || log.node === nodeName).slice(0, limit);
  return { logs, note: overview.system_log_note };
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Handler = (ctx: { req: Connect.IncomingMessage; match: RegExpMatchArray | null; query: URLSearchParams; body?: any }) => unknown;
type Route = { method: string; pattern: RegExp; handler: Handler };

const routes: Route[] = [
  { method: "GET", pattern: /^\/api\/admin\/overview$/, handler: () => overview },
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
      if (template && !template.built_in) Object.assign(template, body, { updated_at: new Date().toISOString() });
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
        draft: body?.draft ?? { rewrites: [] },
        published_revision_id: "",
        published_version: 0,
        published: { rewrites: [] },
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString()
      };
      mihomoProfiles.push(profile);
      mihomoRevisions.set(profile.id, []);
      return profile;
    }
  },
  { method: "GET", pattern: /^\/api\/admin\/mihomo\/profiles\/([^/]+)\/revisions$/, handler: ({ match }) => mihomoRevisions.get(match?.[1] ?? "") ?? [] },
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
  { method: "POST", pattern: /^\/api\/admin\/mihomo\/profiles\/([^/]+)\/preview$/, handler: () => mihomoPreview() },
  {
    method: "PATCH",
    pattern: /^\/api\/admin\/mihomo\/profiles\/([^/]+)$/,
    handler: ({ match, body }) => {
      const profile = mihomoProfiles.find((item) => item.id === match?.[1]);
      if (profile && body?.draft) {
        profile.draft = body.draft;
        profile.updated_at = new Date().toISOString();
      }
      return profile ?? { ok: true };
    }
  },
  {
    method: "POST",
    pattern: /^\/api\/admin\/mihomo\/profiles\/([^/]+)\/publish$/,
    handler: ({ match }): MihomoProfileRevision => {
      const profile = mihomoProfiles.find((item) => item.id === match?.[1])!;
      const revisions = mihomoRevisions.get(profile.id) ?? [];
      const revision: MihomoProfileRevision = {
        id: `mhpr_${Date.now()}`,
        profile_id: profile.id,
        version: revisions.length + 1,
        document: profile.draft,
        created_at: new Date().toISOString()
      };
      revisions.unshift(revision);
      mihomoRevisions.set(profile.id, revisions);
      profile.published = profile.draft;
      profile.published_revision_id = revision.id;
      profile.published_version = revision.version;
      return revision;
    }
  },
  {
    method: "POST",
    pattern: /^\/api\/admin\/mihomo\/profiles\/([^/]+)\/rollback$/,
    handler: ({ match, body }) => {
      const profile = mihomoProfiles.find((item) => item.id === match?.[1])!;
      const revision = (mihomoRevisions.get(profile.id) ?? []).find((item) => item.id === body?.revision_id);
      if (revision) {
        profile.published = revision.document;
        profile.published_revision_id = revision.id;
        profile.published_version = revision.version;
      }
      return profile;
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
  { method: "GET", pattern: /^\/api\/admin\/users$/, handler: ({ query }) => users.filter((user) => query.get("deleted") === "true" ? Boolean(user.deleted_at) : !user.deleted_at) },
  { method: "GET", pattern: /^\/api\/admin\/traffic\/users$/, handler: () => traffic },
  { method: "GET", pattern: /^\/api\/admin\/settings$/, handler: () => settings },
  { method: "PUT", pattern: /^\/api\/admin\/settings$/, handler: () => settings },
  {
    method: "GET",
    pattern: /^\/api\/admin\/network-events$/,
    handler: ({ query }): NetworkEventsResponse => {
      const { limit, offset } = pageParams(query);
      const search = (query.get("search") ?? "").trim().toLowerCase();
      const action = (query.get("action") ?? "").trim().toLowerCase();
      const nodeName = (query.get("node") ?? "").trim();
      const userName = (query.get("user") ?? "").trim();
      const start = Date.parse(query.get("start") ?? "");
      const end = Date.parse(query.get("end") ?? "");
      const filtered = networkEvents
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
      return { events: filtered.slice(offset, offset + limit), total: filtered.length, limit, offset };
    }
  },
  { method: "GET", pattern: /^\/api\/admin\/nodes\/([^/]+)\/status$/, handler: ({ match }) => nodes.find((n) => n.name === decodeURIComponent(match?.[1] ?? "")) ?? nodes[0] },
  {
    method: "POST",
    pattern: /^\/api\/admin\/nodes\/bootstrap$/,
    handler: ({ body }): AdminNodeBootstrap => {
      const name = (body?.name as string) || `node-${nodes.length}`;
      const node: AdminNode = {
        id: `node_${name}`,
        name,
        public_host: (body?.public_host as string) || "",
        api_base_url: "",
        status: "pending",
        sing_box_version: "",
        last_seen_at: "",
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
            node.hosts = hosts;
            node.public_host = hosts[0].host;
          }
        } else if (typeof body.public_host === "string") {
          node.public_host = body.public_host;
          node.hosts = [{ host: body.public_host, tag: "", selected: true }];
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
    method: "POST",
    pattern: /^\/api\/admin\/users\/([^/]+)\/proxies$/,
    handler: ({ match, body }) => {
      const name = decodeURIComponent(match?.[1] ?? "");
      const proxy = proxies.find((p) => p.node_name === body?.node_name && p.name === body?.proxy_name);
      const list = accessFor(name);
      if (proxy && !list.some((a) => a.node_name === proxy.node_name && a.proxy_name === proxy.name)) {
        const access: AdminProxyAccess = {
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
