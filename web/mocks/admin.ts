import type { Connect, Plugin } from "vite";

import type {
  AdminNode,
  AdminNodeBootstrap,
  AdminProxy,
  AdminProxyAccess,
  AdminSettings,
  AdminUser,
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
    public_host: "203.0.113.10",
    api_base_url: "https://203.0.113.10:18080",
    status: "online",
    sing_box_version: "1.9.3",
    last_seen_at: iso(20_000),
    target_version: "v12",
    current_version: "v12",
    apply_status: "applied",
    latest_heartbeat: iso(20_000),
    agent_version: "0.4.1"
  },
  {
    id: "node_frankfurt",
    name: "frankfurt",
    public_host: "198.51.100.22",
    api_base_url: "https://198.51.100.22:18080",
    status: "online",
    sing_box_version: "1.9.3",
    last_seen_at: iso(45_000),
    target_version: "v12",
    current_version: "v11",
    apply_status: "pending",
    latest_heartbeat: iso(45_000),
    agent_version: "0.4.1"
  },
  {
    id: "node_singapore",
    name: "singapore",
    public_host: "192.0.2.31",
    api_base_url: "https://192.0.2.31:18080",
    status: "offline",
    sing_box_version: "1.9.1",
    last_seen_at: iso(3 * HOUR),
    target_version: "v12",
    current_version: "v10",
    apply_status: "error",
    apply_error: "sing-box check failed: timeout dialing reality handshake",
    latest_heartbeat: iso(3 * HOUR),
    agent_version: "0.3.9"
  }
];

const users: AdminUser[] = [
  {
    id: "user_alice",
    name: "alice",
    display_name: "Alice Zhang",
    status: "active",
    global_quota_bytes: 500 * GiB,
    traffic_multiplier: 1,
    expire_at: iso(-30 * DAY),
    proxy_count: 3
  },
  {
    id: "user_bob",
    name: "bob",
    display_name: "Bob Lee",
    status: "active",
    global_quota_bytes: 200 * GiB,
    traffic_multiplier: 1.5,
    expire_at: iso(-7 * DAY),
    proxy_count: 2
  },
  {
    id: "user_carol",
    name: "carol",
    display_name: "Carol Wu",
    status: "disabled",
    global_quota_bytes: 100 * GiB,
    traffic_multiplier: 1,
    expire_at: iso(2 * DAY),
    proxy_count: 1
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

const systemLogs: SystemLog[] = [
  {
    node: "tokyo",
    service: "sing-box",
    level: "info",
    message: "inbound/vless[reality-in]: tcp connection from 100.64.2.5:51234",
    observed_at: iso(2 * MIN),
    ingested_at: iso(90_000)
  },
  {
    node: "frankfurt",
    service: "boxfleet-agent",
    level: "warn",
    message: "config apply pending: waiting for next pull cycle",
    observed_at: iso(5 * MIN),
    ingested_at: iso(4 * MIN)
  },
  {
    node: "singapore",
    service: "boxfleet-agent",
    level: "error",
    message: "heartbeat failed: dial tcp 192.0.2.31:18080: i/o timeout",
    observed_at: iso(3 * HOUR),
    ingested_at: iso(3 * HOUR)
  }
];

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
  settings_json: JSON.stringify({ flow: "xtls-rprx-vision" }),
  inbound_rules_json: "[]",
  outbound_rules_json: "[]",
  route_rules_json: "[]",
  created_at: iso(30 * DAY),
  updated_at: iso(2 * DAY),
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
      updated_at: iso(DAY)
    }));

const connectionInfoFor = (userName: string): UserConnectionInfo => ({
  user: userName,
  nodes: nodes
    .filter((n) => n.status === "online")
    .map((n) => ({
      user: userName,
      node: n.name,
      proxies: proxies
        .filter((p) => p.node_name === n.name && p.enabled)
        .map((p) => ({
          name: p.name,
          type: "vless",
          server: n.public_host,
          server_port: p.listen_port,
          uuid: `00000000-0000-4000-8000-${p.id.replace(/[^0-9a-f]/gi, "").padEnd(12, "0").slice(0, 12)}`,
          flow: "xtls-rprx-vision",
          server_name: "www.cloudflare.com",
          public_key: "0Rsht7y9rH2nMpdJ8m1l8oUuTPwQ9cKuVqz4kf3aXmE",
          short_id: "a1b2c3d4"
        }))
    }))
});

const networkEvents: NetworkEvent[] = Array.from({ length: 24 }, (_, i) => {
  const u = users[i % users.length];
  const n = nodes[i % nodes.length];
  return {
    node_name: n.name,
    user_name: u.name,
    auth_name: `${u.name}@${n.name}`,
    source_ip: `100.64.${i % 4}.${(i * 7) % 254}`,
    target_host: ["api.github.com", "youtube.com", "registry.npmjs.org", "x.com"][i % 4],
    target_port: i % 4 === 0 ? 80 : 443,
    action: i % 9 === 0 ? "reject" : "accept",
    raw_message: "sing-box route: connection matched default outbound",
    count: 1 + (i % 5),
    window_start: iso((i + 1) * 10 * MIN),
    window_end: iso(i * 10 * MIN),
    created_at: iso(i * 10 * MIN)
  };
});

const settings: AdminSettings = { network_event_retention_days: 90 };

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
  ]
};

type Handler = (ctx: { req: Connect.IncomingMessage; match: RegExpMatchArray | null; query: URLSearchParams }) => unknown;
type Route = { method: string; pattern: RegExp; handler: Handler };

const routes: Route[] = [
  { method: "GET", pattern: /^\/api\/admin\/overview$/, handler: () => overview },
  { method: "GET", pattern: /^\/api\/admin\/system-logs$/, handler: (): SystemLogsResponse => ({ logs: systemLogs, note: overview.system_log_note }) },
  { method: "GET", pattern: /^\/api\/admin\/config\/changes$/, handler: () => configChanges },
  { method: "POST", pattern: /^\/api\/admin\/config\/publish$/, handler: () => ({ published: configChanges.changed.map((c) => ({ node: c.node, version: c.target_version })) }) },
  { method: "GET", pattern: /^\/api\/admin\/proxies$/, handler: () => proxies },
  { method: "GET", pattern: /^\/api\/admin\/settings$/, handler: () => settings },
  { method: "PUT", pattern: /^\/api\/admin\/settings$/, handler: () => settings },
  {
    method: "GET",
    pattern: /^\/api\/admin\/network-events$/,
    handler: ({ query }): NetworkEventsResponse => {
      const limit = Number(query.get("limit") ?? 20) || 20;
      const offset = Number(query.get("offset") ?? 0) || 0;
      return { events: networkEvents.slice(offset, offset + limit), total: networkEvents.length, limit, offset };
    }
  },
  { method: "GET", pattern: /^\/api\/admin\/nodes\/([^/]+)\/status$/, handler: ({ match }) => nodes.find((n) => n.name === decodeURIComponent(match?.[1] ?? "")) ?? nodes[0] },
  {
    method: "POST",
    pattern: /^\/api\/admin\/nodes\/bootstrap$/,
    handler: (): AdminNodeBootstrap => ({
      node: nodes[0],
      bootstrap_string: "BFNODE:tokyo:eyJhcGkiOiJodHRwczovLzIwMy4wLjExMy4xMCJ9:devtoken",
      install_script_url: "http://127.0.0.1:18081/install/node.sh"
    })
  },
  { method: "GET", pattern: /^\/api\/admin\/users\/([^/]+)\/proxies$/, handler: ({ match }) => proxyAccessFor(decodeURIComponent(match?.[1] ?? "alice")) },
  { method: "GET", pattern: /^\/api\/admin\/users\/([^/]+)\/connection-info$/, handler: ({ match }) => connectionInfoFor(decodeURIComponent(match?.[1] ?? "alice")) }
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

        for (const route of routes) {
          if (route.method !== method) continue;
          const match = pathname.match(route.pattern);
          if (!match) continue;
          jsonResponse(res, 200, route.handler({ req, match, query }));
          return;
        }

        if (method !== "GET") {
          jsonResponse(res, 200, writeFallback(method));
          return;
        }

        jsonResponse(res, 404, { error: `mock: no fixture for ${method} ${pathname}` });
      });
    }
  };
}
