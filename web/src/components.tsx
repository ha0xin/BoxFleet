import { Link, MoreHorizontal, Pencil, Trash2 } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Note } from "@/components/ui/note";
import { StatusDot, type StatusTone } from "@/components/ui/status-dot";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

import type { AdminNode, AdminProxy, AdminProxyAccess, AdminUser, NetworkEvent } from "./types";
import { formatBytes, formatDuration, formatTime } from "./utils";

function ActionsCell({
  deleteLabel = "Delete",
  editLabel = "Edit",
  label,
  onConnectionInfo,
  onDelete,
  onEdit
}: {
  deleteLabel?: string;
  editLabel?: string;
  label: string;
  onConnectionInfo?: () => void;
  onDelete?: () => void;
  onEdit?: () => void;
}) {
  return (
    <TableCell className="w-10 text-right">
      <DropdownMenu>
        <DropdownMenu.Trigger
          render={
            <button
              type="button"
              aria-label={`Actions for ${label}`}
              className="inline-flex h-7 w-7 items-center justify-center rounded-md text-gray-700 transition-colors hover:bg-gray-alpha-200 hover:text-gray-1000 focus:outline-none"
            >
              <MoreHorizontal size={16} />
            </button>
          }
        />
        <DropdownMenu.Content align="end">
          {onEdit ? (
            <DropdownMenu.Item icon={<Pencil className="h-4 w-4" />} onClick={onEdit}>
              {editLabel}
            </DropdownMenu.Item>
          ) : null}
          {onConnectionInfo ? (
            <DropdownMenu.Item icon={<Link className="h-4 w-4" />} onClick={onConnectionInfo}>
              Connection Info
            </DropdownMenu.Item>
          ) : null}
          {onDelete ? (
            <DropdownMenu.Item icon={<Trash2 className="h-4 w-4" />} variant="danger" onClick={onDelete}>
              {deleteLabel}
            </DropdownMenu.Item>
          ) : null}
        </DropdownMenu.Content>
      </DropdownMenu>
    </TableCell>
  );
}

export function NodesTable({
  nodes,
  compact = false,
  onSelect,
  onDelete,
  selectedName
}: {
  nodes: AdminNode[];
  compact?: boolean;
  onSelect?: (name: string) => void;
  onDelete?: (node: AdminNode) => void;
  selectedName?: string;
}) {
  if (nodes.length === 0) {
    return <EmptyState text="No nodes have been registered." />;
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Host</TableHead>
          <TableHead>Config</TableHead>
          {!compact ? <TableHead>Version</TableHead> : null}
          {!compact ? <TableHead>Last Seen</TableHead> : null}
          {!compact && onSelect ? <TableHead className="w-10" aria-label="Actions" /> : null}
        </TableRow>
      </TableHeader>
      <TableBody>
        {nodes.map((node) => (
          <TableRow key={node.id || node.name} data-state={selectedName === node.name ? "selected" : undefined}>
            <TableCell>
              <span className="font-semibold text-gray-1000">{node.name}</span>
            </TableCell>
            <TableCell>
              <StatusBadge value={nodeStatus(node)} />
            </TableCell>
            <TableCell className="text-gray-900">{node.public_host || "—"}</TableCell>
            <TableCell className="text-gray-900">
              {node.current_version || "—"} / {node.target_version || "—"}
            </TableCell>
            {!compact ? (
              <TableCell>
                {displaySingBoxVersion(node.sing_box_version) || displayAgentVersion(node.agent_version) ? (
                  <span className="inline-flex items-center gap-1.5 text-sm">
                    <span className="text-gray-1000">{displaySingBoxVersion(node.sing_box_version) || "—"}</span>
                    {displayAgentVersion(node.agent_version) ? (
                      <span className="text-gray-700">· agent {displayAgentVersion(node.agent_version)}</span>
                    ) : null}
                  </span>
                ) : (
                  <span className="text-gray-700">—</span>
                )}
              </TableCell>
            ) : null}
            {!compact ? (
              <TableCell className="text-gray-900">{formatTime(node.latest_heartbeat || node.last_seen_at) || "—"}</TableCell>
            ) : null}
            {!compact && onSelect ? (
              <ActionsCell
                label={node.name}
                onDelete={onDelete && node.status !== "disabled" ? () => onDelete(node) : undefined}
                onEdit={() => onSelect(node.name)}
              />
            ) : null}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

export function ProxiesTable({
  onDelete,
  onSelect,
  proxies
}: {
  onDelete?: (proxy: AdminProxy) => void;
  onSelect?: (proxy: AdminProxy) => void;
  proxies: AdminProxy[];
}) {
  if (proxies.length === 0) {
    return <EmptyState text="No proxies have been created." />;
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead>Node</TableHead>
          <TableHead>Protocol</TableHead>
          <TableHead>Listen</TableHead>
          <TableHead>Transport</TableHead>
          <TableHead>Multiplier</TableHead>
          <TableHead>Status</TableHead>
          {onSelect ? <TableHead className="w-10" aria-label="Actions" /> : null}
        </TableRow>
      </TableHeader>
      <TableBody>
        {proxies.map((proxy) => (
          <TableRow key={proxy.id}>
            <TableCell>
              <div className="font-semibold text-gray-1000">{proxy.name}</div>
              <div className="text-xs text-gray-700">{proxy.id}</div>
            </TableCell>
            <TableCell className="text-gray-900">{proxy.node_name}</TableCell>
            <TableCell className="text-gray-900">{proxy.protocol}</TableCell>
            <TableCell className="text-gray-900">{proxy.listen}:{proxy.listen_port}</TableCell>
            <TableCell className="text-gray-900">{proxy.transport}</TableCell>
            <TableCell className="text-gray-900">{proxy.traffic_multiplier.toFixed(2)}x</TableCell>
            <TableCell>
              <StatusBadge value={proxy.enabled ? "active" : "disabled"} />
            </TableCell>
            {onSelect ? (
              <ActionsCell
                label={proxy.name}
                onDelete={onDelete && proxy.enabled ? () => onDelete(proxy) : undefined}
                onEdit={() => onSelect(proxy)}
              />
            ) : null}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

export function NetworkEventsTable({ events, compact = false }: { events: NetworkEvent[]; compact?: boolean }) {
  if (events.length === 0) {
    return <EmptyState text="No network events have been uploaded." />;
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          {!compact ? <TableHead>Node</TableHead> : null}
          <TableHead>User</TableHead>
          <TableHead>Action</TableHead>
          <TableHead>Target</TableHead>
          {!compact ? <TableHead>Source</TableHead> : null}
          {!compact ? <TableHead>Started</TableHead> : null}
          {!compact ? <TableHead>Duration</TableHead> : null}
          <TableHead>Count</TableHead>
          <TableHead>Observed</TableHead>
          {!compact ? <TableHead>Raw</TableHead> : null}
        </TableRow>
      </TableHeader>
      <TableBody>
        {events.map((event, index) => (
          <TableRow key={`${event.created_at}-${event.auth_name}-${index}`}>
            {!compact ? <TableCell className="text-gray-900">{event.node_name || "—"}</TableCell> : null}
            <TableCell className="text-gray-1000">{event.user_name || event.auth_name || "—"}</TableCell>
            <TableCell>
              <StatusBadge value={event.action || "event"} />
            </TableCell>
            <TableCell className="text-gray-900">
              {event.target_host || "—"}
              {event.target_port ? `:${event.target_port}` : ""}
            </TableCell>
            {!compact ? <TableCell className="text-gray-900">{event.source_ip || "—"}</TableCell> : null}
            {!compact ? <TableCell className="text-gray-900">{formatTime(event.window_start) || "—"}</TableCell> : null}
            {!compact ? (
              <TableCell className="text-gray-900">{formatDuration(event.window_start, event.window_end) || "—"}</TableCell>
            ) : null}
            <TableCell className="text-gray-900">{event.count || 1}</TableCell>
            <TableCell className="text-gray-900">{formatTime(event.window_end || event.created_at || event.window_start) || "—"}</TableCell>
            {!compact ? (
              <TableCell>
                <details className="cursor-pointer">
                  <summary className="text-blue-700">View</summary>
                  <pre className="mt-2 max-w-[520px] overflow-auto rounded-md bg-gray-1000 p-3 text-xs text-gray-100">
                    {event.raw_message}
                  </pre>
                </details>
              </TableCell>
            ) : null}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

export function UsersTable({
  onConnectionInfo,
  onDelete,
  onSelect,
  users
}: {
  onConnectionInfo?: (user: AdminUser) => void;
  onDelete?: (user: AdminUser) => void;
  onSelect?: (user: AdminUser) => void;
  users: AdminUser[];
}) {
  if (users.length === 0) {
    return <EmptyState text="No users have been created." />;
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Proxies</TableHead>
          <TableHead>Quota</TableHead>
          <TableHead>Expires</TableHead>
          {onSelect ? <TableHead className="w-10" aria-label="Actions" /> : null}
        </TableRow>
      </TableHeader>
      <TableBody>
        {users.map((user) => (
          <TableRow key={user.id}>
            <TableCell>
              <div className="font-semibold text-gray-1000">{user.name}</div>
              <div className="text-xs text-gray-700">{user.display_name || user.id}</div>
            </TableCell>
            <TableCell>
              <StatusBadge value={user.status} />
            </TableCell>
            <TableCell className="text-gray-900">{user.proxy_count}</TableCell>
            <TableCell className="text-gray-900">
              {user.global_quota_bytes > 0 ? formatBytes(user.global_quota_bytes) : "Unlimited"}
            </TableCell>
            <TableCell className="text-gray-900">{formatTime(user.expire_at) || "Never"}</TableCell>
            {onSelect ? (
              <ActionsCell
                label={user.name}
                onConnectionInfo={onConnectionInfo ? () => onConnectionInfo(user) : undefined}
                onDelete={onDelete && user.status !== "disabled" ? () => onDelete(user) : undefined}
                onEdit={() => onSelect(user)}
              />
            ) : null}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

export function UserAccessTable({
  accesses,
  onRevoke
}: {
  accesses: AdminProxyAccess[];
  onRevoke?: (access: AdminProxyAccess) => void;
}) {
  if (accesses.length === 0) {
    return <EmptyState text="No proxy access has been issued for this user." />;
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Proxy</TableHead>
          <TableHead>Node</TableHead>
          <TableHead>Protocol</TableHead>
          <TableHead>Listen</TableHead>
          <TableHead>Auth</TableHead>
          <TableHead>Status</TableHead>
          {onRevoke ? <TableHead className="w-10" aria-label="Actions" /> : null}
        </TableRow>
      </TableHeader>
      <TableBody>
        {accesses.map((access) => (
          <TableRow key={access.id}>
            <TableCell>
              <div className="font-semibold text-gray-1000">{access.proxy_name}</div>
              <div className="text-xs text-gray-700">{access.transport}</div>
            </TableCell>
            <TableCell className="text-gray-900">{access.node_name}</TableCell>
            <TableCell className="text-gray-900">{access.protocol}</TableCell>
            <TableCell className="text-gray-900">{access.listen}:{access.listen_port}</TableCell>
            <TableCell className="text-gray-900">{access.auth_name}</TableCell>
            <TableCell>
              <StatusBadge value={access.enabled ? "active" : "disabled"} />
            </TableCell>
            {onRevoke && access.enabled ? (
              <ActionsCell
                deleteLabel="Revoke"
                label={access.auth_name}
                onDelete={() => onRevoke(access)}
              />
            ) : onRevoke ? (
              <TableCell className="w-10" />
            ) : null}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

export function Metric({ icon: Icon, label, value }: { icon: LucideIcon; label: string; value: string }) {
  return (
    <Card>
      <CardContent className="flex flex-col gap-2 p-4">
        <Icon size={18} className="text-teal-700" />
        <span className="text-xs text-gray-700">{label}</span>
        <strong className="text-2xl font-semibold tracking-tight text-gray-1000">{value}</strong>
      </CardContent>
    </Card>
  );
}

export function Panel({ action, children, title, padded = false }: { action?: ReactNode; children: ReactNode; title: string; padded?: boolean }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        {action ? <div className="flex shrink-0 items-center gap-2">{action}</div> : null}
      </CardHeader>
      {padded ? <CardContent>{children}</CardContent> : <div>{children}</div>}
    </Card>
  );
}

export function StatusBadge({ value }: { value: string }) {
  const normalized = value.toLowerCase();
  let tone: StatusTone = "red";
  if (["active", "applied", "connect", "ok", "online"].includes(normalized)) tone = "green";
  else if (["pending config"].includes(normalized)) tone = "blue";
  else if (["pending", "unknown", "event"].includes(normalized)) tone = "amber";
  else if (normalized === "disabled") tone = "gray";
  const label = value || "unknown";
  return (
    <span className="inline-flex items-center gap-1.5 text-sm leading-5 text-gray-1000">
      <StatusDot tone={tone} />
      <span className="capitalize">{label}</span>
    </span>
  );
}

export function EmptyState({ text }: { text: string }) {
  return (
    <div className="p-4">
      <Note variant="secondary" size="sm">{text}</Note>
    </div>
  );
}

function displaySingBoxVersion(version: string | undefined) {
  const normalized = (version || "").trim().replace(/^sing-box\s+version\s+/i, "");
  if (!normalized || normalized.toLowerCase() === "unknown") {
    return "";
  }
  return normalized;
}

function displayAgentVersion(version: string | undefined) {
  return (version || "").trim();
}

function nodeStatus(node: AdminNode) {
  if (node.status === "disabled") {
    return "disabled";
  }
  if (node.apply_status === "failed") {
    return "failed";
  }
  if (node.target_version && (node.target_version !== node.current_version || node.apply_status === "pending")) {
    return "pending config";
  }
  if (node.latest_heartbeat || node.last_seen_at) {
    return "online";
  }
  return node.status || "unknown";
}
