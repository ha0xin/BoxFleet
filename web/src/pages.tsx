import {
  Download,
  Gauge as GaugeIcon,
  Plus,
  RefreshCw,
  Server,
  Trash2,
  Upload,
  Users
} from "lucide-react";
import Anser from "anser";
import { diffLines } from "diff";
import { useEffect, useMemo, useRef, useState } from "react";
import type { DateRange } from "react-day-picker";
import { differenceInMilliseconds, format, isValid } from "date-fns";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  flexRender,
  getCoreRowModel,
  type ColumnDef,
  useReactTable
} from "@tanstack/react-table";
import { Controller, useForm } from "react-hook-form";
import { useSearchParams } from "react-router-dom";
import { z } from "zod";

import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { LoadingDots } from "@/components/ui/loading-dots";
import { Note } from "@/components/ui/note";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Snippet } from "@/components/ui/snippet";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";

import {
  EmptyState,
  Metric,
  NodesTable,
  Panel,
  ProxiesTable,
  StatusBadge,
  UserAccessTable,
  UsersTable
} from "./components";
import type {
  AdminNode,
  AdminNodeBootstrap,
  AdminProxy,
  AdminProxyAccess,
  AdminUser,
  AdminSettings,
  NetworkEvent,
  NetworkEventsResponse,
  Overview,
  SystemLogsResponse,
  TrafficRow,
  UserConnectionInfo
} from "./types";
import { formatBytes, formatTime } from "./utils";

type Requester = <T>(path: string, init?: RequestInit) => Promise<T>;

type ConfigChangesResponse = {
  changed: ConfigChange[];
};

type ConfigChange = {
  node: string;
  target_hash: string;
  rendered_hash: string;
  target_version: string;
  target_config: string;
  rendered_config: string;
};

export function OverviewPage({
  activeNodes,
  activeUsers,
  overview,
  trafficRows,
  totalTraffic
}: {
  activeNodes: number;
  activeUsers: number;
  overview: Overview | null;
  trafficRows: TrafficRow[];
  totalTraffic: number;
}) {
  const trafficUsers = groupTrafficRows(trafficRows);
  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Metric icon={Server} label="Active Nodes" value={`${activeNodes}/${overview?.nodes.length ?? 0}`} />
        <Metric icon={Users} label="Active Users" value={`${activeUsers}/${overview?.users.length ?? 0}`} />
        <Metric icon={GaugeIcon} label="Billable Traffic" value={formatBytes(totalTraffic)} />
        <Metric icon={GaugeIcon} label="Traffic Users" value={`${trafficUsers.length}`} />
      </div>
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.25fr)]">
        <Panel title="Node State">
          <NodesTable nodes={overview?.nodes.slice(0, 6) ?? []} compact />
        </Panel>
        <Panel title="User Traffic">
          <UserTrafficTable rows={trafficRows} compact limit={8} />
        </Panel>
      </div>
    </div>
  );
}

export function NodesPage({
  nodes,
  request,
  refresh
}: {
  nodes: AdminNode[];
  request: Requester;
  refresh: () => Promise<void>;
}) {
  const [selectedName, setSelectedName] = useState("");
  const [deleteNode, setDeleteNode] = useState<AdminNode | null>(null);
  const [editOpen, setEditOpen] = useState(false);
  const [addOpen, setAddOpen] = useState(false);
  const [showDisabled, setShowDisabled] = useState(false);
  const [configChanges, setConfigChanges] = useState<ConfigChange[]>([]);
  const [publishOpen, setPublishOpen] = useState(false);
  const [publishing, setPublishing] = useState(false);
  const [publishMessage, setPublishMessage] = useState("");
  const [deleteMessage, setDeleteMessage] = useState("");
  const [deleting, setDeleting] = useState(false);
  const selected = nodes.find((node) => node.name === selectedName);
  const visibleNodes = showDisabled ? nodes : nodes.filter((node) => node.status !== "disabled");

  async function loadConfigChanges() {
    try {
      const response = await request<ConfigChangesResponse>("/api/admin/config/changes");
      setConfigChanges(response.changed);
    } catch {
      setConfigChanges([]);
    }
  }

  useEffect(() => {
    void loadConfigChanges();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nodes.length]);

  async function publishChanges() {
    if (configChanges.length === 0) {
      return;
    }
    setPublishing(true);
    setPublishMessage("");
    try {
      await request<{ published: unknown[] }>("/api/admin/config/publish", { method: "POST" });
      await refresh();
      await loadConfigChanges();
      setPublishOpen(false);
      setPublishMessage("配置已发布，等待节点确认");
    } catch (err) {
      setPublishMessage(err instanceof Error ? err.message : "publish failed");
    } finally {
      setPublishing(false);
    }
  }

  async function confirmDeleteNode() {
    if (!deleteNode) {
      return;
    }
    setDeleting(true);
    setDeleteMessage("");
    try {
      await request<AdminNode>(`/api/admin/nodes/${encodeURIComponent(deleteNode.name)}`, { method: "DELETE" });
      await refresh();
      await loadConfigChanges();
      setDeleteNode(null);
    } catch (err) {
      setDeleteMessage(err instanceof Error ? err.message : "delete node failed");
    } finally {
      setDeleting(false);
    }
  }

  return (
    <>
      <Panel
        action={
          <div className="flex items-center gap-3">
            {configChanges.length > 0 ? (
              <Button
                size="sm"
                className="!border-blue-700 !bg-blue-700 !text-white hover:!border-blue-800 hover:!bg-blue-800"
                onClick={() => setPublishOpen(true)}
              >
                {configChanges.length} changed
              </Button>
            ) : null}
            <div className="flex items-center gap-2">
              <Switch
                checked={showDisabled}
                onCheckedChange={setShowDisabled}
                id="show-disabled"
              />
              <Label htmlFor="show-disabled" className="text-xs text-gray-700">
                Show disabled
              </Label>
            </div>
            <Button size="sm" prefix={<Plus size={14} />} onClick={() => setAddOpen(true)}>
              添加节点
            </Button>
          </div>
        }
        title="Managed Nodes"
      >
        <NodesTable
          nodes={visibleNodes}
          onDelete={setDeleteNode}
          onSelect={(name) => {
            setSelectedName(name);
            setEditOpen(true);
          }}
        />
        {publishMessage ? (
          <div className="border-t border-gray-alpha-400 px-4 py-2 text-xs text-gray-700">{publishMessage}</div>
        ) : null}
      </Panel>
      {addOpen ? (
        <AddNodeModal
          onClose={() => setAddOpen(false)}
          onCreated={() => void refresh()}
          request={request}
        />
      ) : null}
      {editOpen && selected ? (
        <EditNodeModal
          key={selected.name}
          node={selected}
          onClose={() => setEditOpen(false)}
          refresh={refresh}
          request={request}
        />
      ) : null}
      {publishOpen ? (
        <PublishChangesModal
          changes={configChanges}
          onClose={() => setPublishOpen(false)}
          onPublish={() => void publishChanges()}
          publishing={publishing}
        />
      ) : null}
      {deleteNode ? (
        <ConfirmDeleteModal
          error={deleteMessage}
          loading={deleting}
          message={deleteNode.name}
          onClose={() => {
            setDeleteNode(null);
            setDeleteMessage("");
          }}
          onConfirm={() => void confirmDeleteNode()}
          title="Delete Node"
        />
      ) : null}
    </>
  );
}

function EditNodeModal({
  node,
  onClose,
  refresh,
  request
}: {
  node: AdminNode;
  onClose: () => void;
  refresh: () => Promise<void>;
  request: Requester;
}) {
  const [form, setForm] = useState({
    public_host: node.public_host || "",
    api_base_url: node.api_base_url || "",
    status: node.status || "active"
  });
  const [preview, setPreview] = useState("");
  const [previewError, setPreviewError] = useState("");
  const [previewLoading, setPreviewLoading] = useState(false);
  const [message, setMessage] = useState("");
  const [saving, setSaving] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  async function loadPreview() {
    setPreviewLoading(true);
    setPreviewError("");
    try {
      const raw = await request<Record<string, unknown>>(`/api/admin/nodes/${encodeURIComponent(node.name)}/config/render`);
      setPreview(JSON.stringify(raw, null, 2));
    } catch (err) {
      setPreview("");
      setPreviewError(err instanceof Error ? err.message : "render failed");
    } finally {
      setPreviewLoading(false);
    }
  }

  useEffect(() => {
    void loadPreview();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [node.name]);

  async function save() {
    setSaving(true);
    setMessage("");
    try {
      await request<AdminNode>(`/api/admin/nodes/${encodeURIComponent(node.name)}`, {
        method: "PATCH",
        body: JSON.stringify({ name: node.name, ...form })
      });
      setMessage("已保存（未发布）");
      await refresh();
      await loadPreview();
    } catch (err) {
      setMessage(err instanceof Error ? err.message : "save failed");
    } finally {
      setSaving(false);
    }
  }

  async function softDelete() {
    setSaving(true);
    try {
      await request<AdminNode>(`/api/admin/nodes/${encodeURIComponent(node.name)}`, {
        method: "DELETE"
      });
      await refresh();
      onClose();
    } catch (err) {
      setMessage(err instanceof Error ? err.message : "delete failed");
      setSaving(false);
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent size="xl">
        <DialogHeader>
          <DialogTitle>节点：{node.name}</DialogTitle>
        </DialogHeader>
        <div className="grid grid-cols-1 gap-5 lg:grid-cols-[minmax(280px,1fr)_minmax(0,1.4fr)]">
          <div className="flex flex-col gap-4">
            <FieldRow label="Name" hint="名称创建后不可修改" narrow>
              <Input disabled value={node.name} />
            </FieldRow>
            <FieldRow label="Public Host" hint="客户端连接节点用的公网地址 / 域名" narrow>
              <Input
                value={form.public_host}
                onChange={(event) => setForm({ ...form, public_host: event.target.value })}
              />
            </FieldRow>
            <FieldRow label="API Base URL" hint="仅当节点需要单独的回连地址时填写" narrow>
              <Input
                value={form.api_base_url}
                placeholder="可选，默认由 server 推断"
                onChange={(event) => setForm({ ...form, api_base_url: event.target.value })}
              />
            </FieldRow>
            <FieldRow label="Status" narrow>
              <Select value={form.status} onValueChange={(value) => setForm({ ...form, status: value })}>
                <SelectTrigger>
                  <SelectValue placeholder="Status" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="active">active</SelectItem>
                  <SelectItem value="pending">pending</SelectItem>
                  <SelectItem value="disabled">disabled</SelectItem>
                  <SelectItem value="degraded">degraded</SelectItem>
                </SelectContent>
              </Select>
            </FieldRow>
          </div>
          <div className="flex min-w-0 flex-col gap-2">
            <div className="flex items-center justify-between text-xs font-semibold text-gray-700">
              <span>配置预览（实时渲染，未发布）</span>
              <Button
                size="tiny"
                variant="secondary"
                disabled={previewLoading}
                prefix={<RefreshCw size={12} className={previewLoading ? "animate-spin" : ""} />}
                onClick={() => void loadPreview()}
              >
                刷新
              </Button>
            </div>
            {previewError ? (
              <Note variant="error" size="sm">{previewError}</Note>
            ) : (
              <pre className="m-0 max-h-[480px] overflow-auto rounded-md bg-gray-1000 p-3 font-mono text-xs leading-5 text-gray-100">
                {preview || "（加载中…）"}
              </pre>
            )}
          </div>
        </div>
        <DialogFooter>
          <div className="flex w-full flex-col items-stretch gap-2 sm:flex-row sm:items-center sm:justify-between">
            {confirmDelete ? (
              <div className="flex items-center gap-2">
                <span className="text-xs text-gray-700">确认禁用此节点？</span>
                <Button size="sm" variant="secondary" onClick={() => setConfirmDelete(false)}>
                  取消
                </Button>
                <Button
                  size="sm"
                  variant="error"
                  disabled={saving}
                  prefix={<Trash2 size={14} />}
                  onClick={() => void softDelete()}
                >
                  确认禁用
                </Button>
              </div>
            ) : (
              <Button
                size="sm"
                variant="tertiary"
                className="text-red-800 hover:bg-red-100"
                disabled={saving || node.status === "disabled"}
                prefix={<Trash2 size={14} />}
                onClick={() => setConfirmDelete(true)}
              >
                删除节点
              </Button>
            )}
            <div className="flex items-center gap-2">
              {message ? <span className="text-xs text-gray-700">{message}</span> : null}
              <Button
                size="sm"
                disabled={saving}
                loading={saving}
                onClick={() => void save()}
              >
                保存
              </Button>
            </div>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function PublishChangesModal({
  changes,
  onClose,
  onPublish,
  publishing
}: {
  changes: ConfigChange[];
  onClose: () => void;
  onPublish: () => void;
  publishing: boolean;
}) {
  const [selectedNode, setSelectedNode] = useState(changes[0]?.node || "");
  const selected = changes.find((change) => change.node === selectedNode) || changes[0];

  useEffect(() => {
    if (!changes.some((change) => change.node === selectedNode)) {
      setSelectedNode(changes[0]?.node || "");
    }
  }, [changes, selectedNode]);

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent size="xl">
        <DialogHeader>
          <DialogTitle>Publish Config Changes</DialogTitle>
        </DialogHeader>
        <div className="flex min-h-0 flex-col gap-4">
          <div className="flex flex-wrap items-center gap-2">
            {changes.map((change) => (
              <Button
                key={change.node}
                size="tiny"
                variant={change.node === selected?.node ? "default" : "secondary"}
                onClick={() => setSelectedNode(change.node)}
              >
                {change.node}
              </Button>
            ))}
          </div>
          {selected ? (
            <div className="flex min-w-0 flex-col gap-2">
              <div className="flex items-center justify-between text-xs text-gray-700">
                <span>
                  {selected.target_version ? `published v${selected.target_version}` : "no published config"} {"->"} unpublished render
                </span>
                <span className="font-mono">
                  {shortHash(selected.target_hash) || "none"} {"->"} {shortHash(selected.rendered_hash)}
                </span>
              </div>
              <DiffView change={selected} />
            </div>
          ) : (
            <Note variant="secondary" size="sm">No config changes.</Note>
          )}
        </div>
        <DialogFooter>
          <Button size="sm" variant="secondary" disabled={publishing} onClick={onClose}>
            Cancel
          </Button>
          <Button size="sm" loading={publishing} disabled={changes.length === 0 || publishing} onClick={onPublish}>
            Publish
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function shortHash(hash: string) {
  return hash ? hash.slice(0, 8) : "";
}

function DiffView({ change }: { change: ConfigChange }) {
  const before = change.target_config || "";
  const after = change.rendered_config || "";
  const diff = compactDiff(lineDiff(before, after), 3);
  return (
    <pre className="m-0 max-h-[520px] overflow-auto rounded-md border border-gray-alpha-400 bg-gray-100 p-0 font-mono text-xs leading-5 text-gray-1000">
      <div className="px-3 py-1 text-gray-700">--- Current published config for {change.node}</div>
      <div className="px-3 py-1 text-gray-700">+++ New unpublished config for {change.node}</div>
      {diff.map((part, index) => (
        <div
          key={`${index}-${part.kind}-${part.line}`}
          className={
            part.kind === "+"
              ? "bg-blue-100 px-3 text-blue-900"
              : part.kind === "-"
                ? "bg-red-100 px-3 text-red-900"
                : part.kind === "@"
                  ? "border-y border-gray-alpha-400 bg-background-200 px-3 text-gray-700"
                : "px-3 text-gray-900"
          }
        >
          {part.kind}
          {part.line || " "}
        </div>
      ))}
    </pre>
  );
}

type DiffPart = { kind: " " | "+" | "-" | "@"; line: string };

function lineDiff(before: string, after: string) {
  return diffLines(before, after, { newlineIsToken: false }).flatMap((part) => {
    const kind: DiffPart["kind"] = part.added ? "+" : part.removed ? "-" : " ";
    return part.value
      .replace(/\n$/, "")
      .split("\n")
      .map((line) => ({ kind, line }));
  });
}

function compactDiff(parts: DiffPart[], contextLines: number) {
  const changed = new Set<number>();
  parts.forEach((part, index) => {
    if (part.kind === "+" || part.kind === "-") {
      for (let offset = -contextLines; offset <= contextLines; offset += 1) {
        const visible = index + offset;
        if (visible >= 0 && visible < parts.length) {
          changed.add(visible);
        }
      }
    }
  });
  if (changed.size === 0) {
    return parts.slice(0, 80);
  }
  const out: DiffPart[] = [];
  let previous = -1;
  Array.from(changed)
    .sort((a, b) => a - b)
    .forEach((index) => {
      if (previous !== -1 && index > previous + 1) {
        out.push({ kind: "@", line: "..." });
      }
      out.push(parts[index]);
      previous = index;
    });
  return out;
}

function AddNodeModal({
  onClose,
  onCreated,
  request
}: {
  onClose: () => void;
  onCreated: () => void;
  request: Requester;
}) {
  const [step, setStep] = useState<"form" | "generated">("form");
  const [nodeName, setNodeName] = useState("");
  const [description, setDescription] = useState("");
  const [joinString, setJoinString] = useState("");
  const [installScriptURL, setInstallScriptURL] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const [errorMsg, setErrorMsg] = useState("");
  const [nodeStatus, setNodeStatus] = useState<"pending" | "online" | "offline">("pending");
  const [createdAt, setCreatedAt] = useState<number | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const createdAtRef = useRef<number | null>(null);
  const bootstrapCommand = useMemo(
    () => [
      `curl -fsSL ${shellQuote(installScriptURL)} -o /tmp/boxfleet-install.sh`,
      `sudo sh /tmp/boxfleet-install.sh ${shellQuote(joinString)}`
    ],
    [installScriptURL, joinString]
  );

  async function checkStatus() {
    try {
      const node = await request<AdminNode>(`/api/admin/nodes/${encodeURIComponent(nodeName)}/status`);
      const online = Boolean(node.latest_heartbeat) || node.status === "active" || node.apply_status === "applied";
      if (online) {
        setNodeStatus("online");
        return true;
      }
      const created = createdAtRef.current;
      if (created && Date.now() - created > 5 * 60 * 1000) {
        setNodeStatus("offline");
      } else {
        setNodeStatus("pending");
      }
      return false;
    } catch {
      return false;
    }
  }

  useEffect(() => {
    if (step !== "generated" || !nodeName) {
      return;
    }
    const interval = setInterval(async () => {
      const ok = await checkStatus();
      if (ok) {
        clearInterval(interval);
      }
    }, 8000);
    return () => clearInterval(interval);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step, nodeName]);

  async function handleGenerate() {
    const trimmed = nodeName.trim();
    if (!trimmed) {
      return;
    }
    setIsLoading(true);
    setErrorMsg("");
    try {
      const payload = await request<AdminNodeBootstrap>("/api/admin/nodes/bootstrap", {
        method: "POST",
        body: JSON.stringify({ name: trimmed })
      });
      setJoinString(payload.bootstrap_string);
      setInstallScriptURL(payload.install_script_url);
      setStep("generated");
      setNodeStatus("pending");
      const now = Date.now();
      setCreatedAt(now);
      createdAtRef.current = now;
      onCreated();
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : "生成失败，请稍后重试");
    } finally {
      setIsLoading(false);
    }
  }

  async function handleManualRefresh() {
    setRefreshing(true);
    await checkStatus();
    setRefreshing(false);
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent size="lg">
        <DialogHeader>
          <DialogTitle>添加新节点</DialogTitle>
        </DialogHeader>
        {step === "form" ? (
          <div className="flex flex-col gap-4">
            <FieldRow label="节点名称" required hint={!nodeName.trim() ? "名称不能为空" : undefined} hintTone="warn">
              <Input
                autoFocus
                maxLength={64}
                placeholder="hk-node-01"
                value={nodeName}
                onChange={(event) => setNodeName(event.target.value)}
                containerClassName="max-w-xs"
              />
            </FieldRow>
            <FieldRow label="描述">
              <Textarea
                rows={3}
                placeholder="香港住宅 IP 节点，用于 ChatGPT 解锁"
                value={description}
                onChange={(event) => setDescription(event.target.value)}
              />
            </FieldRow>
            {errorMsg ? <Note variant="error" size="sm">{errorMsg}</Note> : null}
          </div>
        ) : (
          <div className="flex flex-col gap-4">
            <Note variant="success" size="md">
              接入字符串已生成。安装脚本会拉取 agent 和 sing-box，然后执行 bootstrap
            </Note>

            <div className="flex flex-col gap-2">
              <div className="text-xs font-semibold text-gray-700">复制以下字符串到你的服务器</div>
              <Snippet wrap text={joinString} />
            </div>

            <div className="border-t border-gray-alpha-400 pt-3">
              <div className="mb-2 text-xs font-semibold text-gray-1000">下一步：在服务器执行</div>
              <Snippet wrap text={bootstrapCommand} />
            </div>

            <StatusCard
              status={nodeStatus}
              nodeName={nodeName}
              refreshing={refreshing}
              onManualRefresh={() => void handleManualRefresh()}
              onView={onClose}
            />

            {description ? <div className="text-xs text-gray-700">描述：{description}</div> : null}
            {errorMsg ? <Note variant="error" size="sm">{errorMsg}</Note> : null}
          </div>
        )}
        <DialogFooter>
          {step === "form" ? (
            <Button
              size="sm"
              disabled={!nodeName.trim() || isLoading}
              loading={isLoading}
              onClick={() => void handleGenerate()}
            >
              下一步
            </Button>
          ) : (
            <Button size="sm" variant="secondary" onClick={onClose}>
              关闭
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function shellQuote(value: string) {
  return `'${value.replace(/'/g, "'\\''")}'`;
}

function StatusCard({
  status,
  nodeName,
  refreshing,
  onManualRefresh,
  onView
}: {
  status: "pending" | "online" | "offline";
  nodeName: string;
  refreshing: boolean;
  onManualRefresh: () => void;
  onView: () => void;
}) {
  if (status === "online") {
    return (
      <Note
        variant="success"
        size="md"
        fill
        action={
          <Button
            size="sm"
            className="!border-blue-700 !bg-blue-700 !text-white hover:!border-blue-800 hover:!bg-blue-800"
            onClick={onView}
          >
            查看节点详情
          </Button>
        }
      >
        节点已成功上线！名称：{nodeName}
      </Note>
    );
  }
  if (status === "offline") {
    return (
      <Note
        variant="warning"
        size="md"
        fill
        action={
          <Button
            size="sm"
            variant="secondary"
            disabled={refreshing}
            prefix={<RefreshCw size={13} className={refreshing ? "animate-spin" : ""} />}
            onClick={onManualRefresh}
          >
            重新检查
          </Button>
        }
      >
        节点暂未上线，请确认服务器已执行 bootstrap 命令
      </Note>
    );
  }
  return (
    <div className="flex items-center justify-between gap-3 border-t border-gray-alpha-400 pt-3">
      <span className="inline-flex min-w-0 items-center gap-2 text-sm text-gray-900">
        正在等待节点连接控制面
        <LoadingDots />
      </span>
      <div className="shrink-0">
        <Button
          size="sm"
          variant="secondary"
          disabled={refreshing}
          prefix={<RefreshCw size={13} className={refreshing ? "animate-spin" : ""} />}
          onClick={onManualRefresh}
        >
          立即检查
        </Button>
      </div>
    </div>
  );
}

function FieldRow({
  label,
  required,
  hint,
  hintTone,
  narrow,
  children
}: {
  label: string;
  required?: boolean;
  hint?: string;
  hintTone?: "warn";
  narrow?: boolean;
  children: React.ReactNode;
}) {
  return (
    <label className={`flex flex-col gap-1.5 ${narrow ? "max-w-xs" : ""}`}>
      <span className="text-sm font-medium text-gray-1000">
        {label}
        {required ? <em className="ml-1 not-italic text-red-800">*</em> : null}
      </span>
      {children}
      {hint ? (
        <span className={hintTone === "warn" ? "text-xs text-red-800" : "text-xs text-gray-700"}>{hint}</span>
      ) : null}
    </label>
  );
}

export function ProxiesPage({ nodes, request }: { nodes: AdminNode[]; request: Requester }) {
  const [proxies, setProxies] = useState<AdminProxy[]>([]);
  const [selectedProxy, setSelectedProxy] = useState<AdminProxy | null>(null);
  const [deleteProxy, setDeleteProxy] = useState<AdminProxy | null>(null);
  const [nodeName, setNodeName] = useState(nodes[0]?.name ?? "");
  const [proxyForm, setProxyForm] = useState(defaultProxyForm());
  const [realitySettings, setRealitySettings] = useState(defaultRealitySettings());
  const [modalOpen, setModalOpen] = useState(false);
  const [showDisabled, setShowDisabled] = useState(false);
  const [message, setMessage] = useState("");
  const [deleteMessage, setDeleteMessage] = useState("");
  const [savingProxy, setSavingProxy] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const visibleProxies = showDisabled ? proxies : proxies.filter((proxy) => proxy.enabled);

  useEffect(() => {
    void loadProxies();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!nodeName && nodes[0]) {
      setNodeName(nodes[0].name);
    }
  }, [nodes, nodeName]);

  async function loadProxies() {
    const rows = await request<AdminProxy[]>("/api/admin/proxies");
    setProxies(rows);
  }

  function openNewProxy() {
    setSelectedProxy(null);
    setProxyForm(defaultProxyForm());
    setRealitySettings(defaultRealitySettings());
    setNodeName(nodes[0]?.name ?? "");
    setMessage("");
    setModalOpen(true);
  }

  function openProxy(proxy: AdminProxy) {
    setSelectedProxy(proxy);
    setProxyForm(proxyFormFromProxy(proxy));
    setRealitySettings(realitySettingsFromJSON(proxy.settings_json));
    setNodeName(proxy.node_name);
    setMessage("");
    setModalOpen(true);
  }

  async function saveProxy() {
    if (savingProxy) {
      return;
    }
    const targetNode = selectedProxy?.node_name || nodeName;
    if (!targetNode) {
      setMessage("Select a node first.");
      return;
    }
    const path = selectedProxy
      ? `/api/admin/nodes/${encodeURIComponent(selectedProxy.node_name)}/proxies/${encodeURIComponent(selectedProxy.name)}`
      : `/api/admin/nodes/${encodeURIComponent(targetNode)}/proxies`;
    setSavingProxy(true);
    setMessage("");
    try {
      await request<AdminProxy>(path, {
        method: selectedProxy ? "PATCH" : "POST",
        body: JSON.stringify({
          ...proxyForm,
          settings_json: JSON.stringify(proxySettingsPayload(realitySettings))
        })
      });
      setMessage(selectedProxy ? "Proxy saved." : "Proxy created.");
      await loadProxies();
    } catch (err) {
      setMessage(err instanceof Error ? err.message : "save proxy failed");
    } finally {
      setSavingProxy(false);
    }
  }

  async function confirmDeleteProxy() {
    if (!deleteProxy) {
      return;
    }
    setDeleting(true);
    setDeleteMessage("");
    try {
      await request<AdminProxy>(
        `/api/admin/nodes/${encodeURIComponent(deleteProxy.node_name)}/proxies/${encodeURIComponent(deleteProxy.name)}`,
        { method: "DELETE" }
      );
      await loadProxies();
      setDeleteProxy(null);
    } catch (err) {
      setDeleteMessage(err instanceof Error ? err.message : "delete proxy failed");
    } finally {
      setDeleting(false);
    }
  }

  return (
    <>
      <Panel
        action={
          <>
            <div className="flex items-center gap-2">
              <Switch
                checked={showDisabled}
                onCheckedChange={setShowDisabled}
                id="show-disabled-proxies"
              />
              <Label htmlFor="show-disabled-proxies" className="text-xs text-gray-700">
                Show disabled
              </Label>
            </div>
            <Button size="sm" disabled={nodes.length === 0} prefix={<Plus size={14} />} onClick={openNewProxy}>
              Add Proxy
            </Button>
          </>
        }
        title="Managed Proxies"
      >
        <ProxiesTable onDelete={setDeleteProxy} onSelect={openProxy} proxies={visibleProxies} />
      </Panel>
      {modalOpen ? (
        <Dialog open onOpenChange={(open) => !open && setModalOpen(false)}>
          <DialogContent size="xl">
            <DialogHeader>
              <DialogTitle>{selectedProxy ? `Proxy: ${selectedProxy.name}` : "Add Proxy"}</DialogTitle>
            </DialogHeader>
            <div className="flex flex-col gap-4">
              <FieldRow label="Node" narrow>
                <Select
                  disabled={Boolean(selectedProxy)}
                  value={nodeName}
                  onValueChange={setNodeName}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="Select node" />
                  </SelectTrigger>
                  <SelectContent>
                    {nodes.map((node) => (
                      <SelectItem key={node.id} value={node.name}>{node.name}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </FieldRow>
              <FieldRow label="Name" narrow>
                <Input
                  disabled={Boolean(selectedProxy)}
                  value={proxyForm.name}
                  onChange={(event) => setProxyForm({ ...proxyForm, name: event.target.value })}
                />
              </FieldRow>
              <FieldRow label="Protocol" narrow>
                <Select
                  disabled={Boolean(selectedProxy)}
                  value={proxyForm.protocol}
                  onValueChange={(value) => setProxyForm({ ...proxyForm, protocol: value })}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="Protocol" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="vless_reality">vless_reality</SelectItem>
                  </SelectContent>
                </Select>
              </FieldRow>
              <FieldRow label="Listen" narrow>
                <Input
                  value={proxyForm.listen}
                  onChange={(event) => setProxyForm({ ...proxyForm, listen: event.target.value })}
                />
              </FieldRow>
              <FieldRow label="Port" narrow>
                <Input
                  type="number"
                  value={proxyForm.listen_port}
                  onChange={(event) => setProxyForm({ ...proxyForm, listen_port: Number(event.target.value) })}
                />
              </FieldRow>
              <FieldRow label="Multiplier" narrow>
                <Input
                  type="number"
                  min={0}
                  step={0.01}
                  value={proxyForm.traffic_multiplier}
                  onChange={(event) => setProxyForm({ ...proxyForm, traffic_multiplier: Number(event.target.value) })}
                />
              </FieldRow>
              {selectedProxy ? (
                <FieldRow label="Transport" narrow>
                  <Input disabled value={selectedProxy.transport} />
                </FieldRow>
              ) : null}
              <div className="flex items-center gap-2">
                <Switch
                  id="proxy-enabled"
                  checked={proxyForm.enabled}
                  onCheckedChange={(checked) => setProxyForm({ ...proxyForm, enabled: checked })}
                />
                <Label htmlFor="proxy-enabled">Enabled</Label>
              </div>
              <section className="border-t border-gray-alpha-400 pt-4">
                <div className="mb-3 text-sm font-semibold text-gray-1000">Reality</div>
                <div className="flex flex-col gap-4">
                  <FieldRow label="SNI / Server Name" hint="客户端 Reality server_name" narrow>
                    <Input
                      value={realitySettings.server_name}
                      onChange={(event) => setRealitySettings({ ...realitySettings, server_name: event.target.value })}
                    />
                  </FieldRow>
                  <FieldRow label="Handshake Server" hint="留空时使用 SNI" narrow>
                    <Input
                      value={realitySettings.handshake_server}
                      onChange={(event) => setRealitySettings({ ...realitySettings, handshake_server: event.target.value })}
                    />
                  </FieldRow>
                  <FieldRow label="Handshake Port" narrow>
                    <Input
                      type="number"
                      min={1}
                      max={65535}
                      value={realitySettings.handshake_port}
                      onChange={(event) => setRealitySettings({ ...realitySettings, handshake_port: Number(event.target.value) })}
                    />
                  </FieldRow>
                  <FieldRow label="Short ID" hint="0 到 8 位十六进制；留空也可以" narrow>
                    <Input
                      value={realitySettings.short_id}
                      onChange={(event) => setRealitySettings({ ...realitySettings, short_id: event.target.value })}
                    />
                  </FieldRow>
                </div>
              </section>
              <details className="border-t border-gray-alpha-400 pt-4">
                <summary className="cursor-pointer text-sm font-semibold text-gray-1000">Advanced</summary>
                <div className="mt-4 flex flex-col gap-4">
                  <FieldRow label="Reality Private Key" hint="新建时留空会自动生成" narrow>
                    <Input
                      type="password"
                      value={realitySettings.reality_private_key}
                      onChange={(event) => setRealitySettings({ ...realitySettings, reality_private_key: event.target.value })}
                    />
                  </FieldRow>
                  <FieldRow label="Reality Public Key" hint="新建时留空会自动生成" narrow>
                    <Input
                      value={realitySettings.reality_public_key}
                      onChange={(event) => setRealitySettings({ ...realitySettings, reality_public_key: event.target.value })}
                    />
                  </FieldRow>
                  <FieldRow label="Inbound Rules JSON">
                    <Textarea
                      rows={4}
                      className="font-mono text-xs"
                      value={proxyForm.inbound_rules_json}
                      onChange={(event) => setProxyForm({ ...proxyForm, inbound_rules_json: event.target.value })}
                    />
                  </FieldRow>
                  <FieldRow label="Outbound Rules JSON">
                    <Textarea
                      rows={4}
                      className="font-mono text-xs"
                      value={proxyForm.outbound_rules_json}
                      onChange={(event) => setProxyForm({ ...proxyForm, outbound_rules_json: event.target.value })}
                    />
                  </FieldRow>
                  <FieldRow label="Route Rules JSON">
                    <Textarea
                      rows={4}
                      className="font-mono text-xs"
                      value={proxyForm.route_rules_json}
                      onChange={(event) => setProxyForm({ ...proxyForm, route_rules_json: event.target.value })}
                    />
                  </FieldRow>
                </div>
              </details>
            </div>
            <DialogFooter>
              {message ? <span className="mr-auto text-xs text-gray-700">{message}</span> : null}
              <Button size="sm" loading={savingProxy} disabled={savingProxy} onClick={() => void saveProxy()}>
                Save Proxy
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      ) : null}
      {deleteProxy ? (
        <ConfirmDeleteModal
          error={deleteMessage}
          loading={deleting}
          message={`${deleteProxy.node_name} / ${deleteProxy.name}`}
          onClose={() => {
            setDeleteProxy(null);
            setDeleteMessage("");
          }}
          onConfirm={() => void confirmDeleteProxy()}
          title="Delete Proxy"
        />
      ) : null}
    </>
  );
}

function defaultProxyForm() {
  return {
    name: "",
    protocol: "vless_reality",
    listen: "::",
    listen_port: 39090,
    enabled: true,
    traffic_multiplier: 1,
    settings_json: "",
    inbound_rules_json: "[]",
    outbound_rules_json: "[]",
    route_rules_json: "[]"
  };
}

function proxyFormFromProxy(proxy: AdminProxy) {
  return {
    name: proxy.name,
    protocol: proxy.protocol,
    listen: proxy.listen,
    listen_port: proxy.listen_port,
    enabled: proxy.enabled,
    traffic_multiplier: proxy.traffic_multiplier,
    settings_json: proxy.settings_json,
    inbound_rules_json: proxy.inbound_rules_json,
    outbound_rules_json: proxy.outbound_rules_json,
    route_rules_json: proxy.route_rules_json
  };
}

function defaultRealitySettings() {
  return {
    server_name: "www.amazon.com",
    handshake_server: "www.amazon.com",
    handshake_port: 443,
    short_id: "",
    reality_private_key: "",
    reality_public_key: ""
  };
}

function realitySettingsFromJSON(raw: string) {
  const defaults = defaultRealitySettings();
  try {
    const parsed = JSON.parse(raw || "{}") as Partial<ReturnType<typeof defaultRealitySettings>>;
    return {
      server_name: typeof parsed.server_name === "string" && parsed.server_name ? parsed.server_name : defaults.server_name,
      handshake_server:
        typeof parsed.handshake_server === "string" && parsed.handshake_server
          ? parsed.handshake_server
          : typeof parsed.server_name === "string" && parsed.server_name
            ? parsed.server_name
            : defaults.handshake_server,
      handshake_port:
        typeof parsed.handshake_port === "number" && parsed.handshake_port > 0
          ? parsed.handshake_port
          : defaults.handshake_port,
      short_id: typeof parsed.short_id === "string" ? parsed.short_id : defaults.short_id,
      reality_private_key:
        typeof parsed.reality_private_key === "string" ? parsed.reality_private_key : defaults.reality_private_key,
      reality_public_key:
        typeof parsed.reality_public_key === "string" ? parsed.reality_public_key : defaults.reality_public_key
    };
  } catch {
    return defaults;
  }
}

function proxySettingsPayload(settings: ReturnType<typeof defaultRealitySettings>) {
  return {
    server_name: settings.server_name.trim(),
    handshake_server: settings.handshake_server.trim() || settings.server_name.trim(),
    handshake_port: settings.handshake_port || 443,
    short_id: settings.short_id.trim(),
    reality_private_key: settings.reality_private_key.trim(),
    reality_public_key: settings.reality_public_key.trim()
  };
}

export function UsersPage({
  refresh,
  request,
  users
}: {
  refresh: () => Promise<void>;
  request: Requester;
  users: AdminUser[];
}) {
  const [selectedUser, setSelectedUser] = useState<AdminUser | null>(null);
  const [accesses, setAccesses] = useState<AdminProxyAccess[]>([]);
  const [addOpen, setAddOpen] = useState(false);
  const [grantOpen, setGrantOpen] = useState(false);
  const [connectionUser, setConnectionUser] = useState<AdminUser | null>(null);
  const [deleteUser, setDeleteUser] = useState<AdminUser | null>(null);
  const [revokeAccess, setRevokeAccess] = useState<AdminProxyAccess | null>(null);
  const [deleteMessage, setDeleteMessage] = useState("");
  const [deleting, setDeleting] = useState(false);
  const [showDisabledUsers, setShowDisabledUsers] = useState(false);
  const [showRevokedAccesses, setShowRevokedAccesses] = useState(false);
  const visibleUsers = showDisabledUsers ? users : users.filter((user) => user.status !== "disabled");
  const visibleAccesses = showRevokedAccesses ? accesses : accesses.filter((access) => access.enabled);

  async function openUser(user: AdminUser) {
    setSelectedUser(user);
    const rows = await request<AdminProxyAccess[]>(`/api/admin/users/${encodeURIComponent(user.name)}/proxies`);
    setAccesses(rows);
  }

  async function confirmDeleteUser() {
    if (!deleteUser) {
      return;
    }
    setDeleting(true);
    setDeleteMessage("");
    try {
      await request<AdminUser>(`/api/admin/users/${encodeURIComponent(deleteUser.name)}`, { method: "DELETE" });
      if (selectedUser?.name === deleteUser.name) {
        setSelectedUser(null);
      }
      await refresh();
      setDeleteUser(null);
    } catch (err) {
      setDeleteMessage(err instanceof Error ? err.message : "delete user failed");
    } finally {
      setDeleting(false);
    }
  }

  async function confirmRevokeAccess() {
    if (!revokeAccess || !selectedUser) {
      return;
    }
    setDeleting(true);
    setDeleteMessage("");
    try {
      await request<AdminProxyAccess>(
        `/api/admin/users/${encodeURIComponent(revokeAccess.user_name)}/proxies/${encodeURIComponent(revokeAccess.node_name)}/${encodeURIComponent(revokeAccess.proxy_name)}`,
        { method: "DELETE" }
      );
      await openUser(selectedUser);
      await refresh();
      setRevokeAccess(null);
    } catch (err) {
      setDeleteMessage(err instanceof Error ? err.message : "revoke access failed");
    } finally {
      setDeleting(false);
    }
  }

  return (
    <>
      <Panel
        action={
          <>
            <div className="flex items-center gap-2">
              <Switch
                checked={showDisabledUsers}
                onCheckedChange={setShowDisabledUsers}
                id="show-disabled-users"
              />
              <Label htmlFor="show-disabled-users" className="text-xs text-gray-700">
                Show disabled
              </Label>
            </div>
            <Button size="sm" prefix={<Plus size={14} />} onClick={() => setAddOpen(true)}>
              Add User
            </Button>
          </>
        }
        title="Proxy Users"
      >
        <UsersTable
          onDelete={setDeleteUser}
          users={visibleUsers}
          onConnectionInfo={(user) => setConnectionUser(user)}
          onSelect={(user) => void openUser(user)}
        />
      </Panel>
      {addOpen ? (
        <AddUserModal
          onClose={() => setAddOpen(false)}
          onCreated={() => void refresh()}
          request={request}
        />
      ) : null}
      {selectedUser ? (
        <Dialog open onOpenChange={(open) => !open && setSelectedUser(null)}>
          <DialogContent size="xl">
            <DialogHeader>
              <DialogTitle>User: {selectedUser.name}</DialogTitle>
            </DialogHeader>
            <div className="flex flex-col gap-4">
              <Card>
                <CardHeader>
                  <CardTitle>Proxy Access</CardTitle>
                  <div className="flex items-center gap-2">
                    <div className="flex items-center gap-2">
                      <Switch
                        checked={showRevokedAccesses}
                        onCheckedChange={setShowRevokedAccesses}
                        id="show-revoked-accesses"
                      />
                      <Label htmlFor="show-revoked-accesses" className="text-xs text-gray-700">
                        Show revoked
                      </Label>
                    </div>
                    <Button size="sm" variant="secondary" prefix={<Plus size={14} />} onClick={() => setGrantOpen(true)}>
                      Grant Access
                    </Button>
                  </div>
                </CardHeader>
                <UserAccessTable accesses={visibleAccesses} onRevoke={setRevokeAccess} />
              </Card>
              <Card>
                <CardHeader>
                  <CardTitle>User</CardTitle>
                </CardHeader>
                <CardContent>
                  <dl className="grid grid-cols-[120px_1fr] gap-y-2 text-sm">
                    <dt className="text-gray-700">Status</dt>
                    <dd><StatusBadge value={selectedUser.status} /></dd>
                    <dt className="text-gray-700">Quota</dt>
                    <dd className="text-gray-1000">
                      {selectedUser.global_quota_bytes > 0
                        ? formatBytes(selectedUser.global_quota_bytes)
                        : "Unlimited"}
                    </dd>
                    <dt className="text-gray-700">Expires</dt>
                    <dd className="text-gray-1000">{formatTime(selectedUser.expire_at) || "Never"}</dd>
                  </dl>
                </CardContent>
              </Card>
            </div>
          </DialogContent>
        </Dialog>
      ) : null}
      {grantOpen && selectedUser ? (
        <GrantAccessModal
          existingAccesses={accesses}
          onClose={() => setGrantOpen(false)}
          onGranted={async () => {
            await openUser(selectedUser);
            await refresh();
          }}
          request={request}
          user={selectedUser}
        />
      ) : null}
      {connectionUser ? (
        <ConnectionInfoModal
          onClose={() => setConnectionUser(null)}
          request={request}
          user={connectionUser}
        />
      ) : null}
      {deleteUser ? (
        <ConfirmDeleteModal
          error={deleteMessage}
          loading={deleting}
          message={deleteUser.name}
          onClose={() => {
            setDeleteUser(null);
            setDeleteMessage("");
          }}
          onConfirm={() => void confirmDeleteUser()}
          title="Delete User"
        />
      ) : null}
      {revokeAccess ? (
        <ConfirmDeleteModal
          actionLabel="Revoke"
          error={deleteMessage}
          loading={deleting}
          message={`${revokeAccess.node_name} / ${revokeAccess.proxy_name} / ${revokeAccess.auth_name}`}
          onClose={() => {
            setRevokeAccess(null);
            setDeleteMessage("");
          }}
          onConfirm={() => void confirmRevokeAccess()}
          title="Revoke Access"
        />
      ) : null}
    </>
  );
}

function ConnectionInfoModal({
  onClose,
  request,
  user
}: {
  onClose: () => void;
  request: Requester;
  user: AdminUser;
}) {
  const [info, setInfo] = useState<UserConnectionInfo | null>(null);
  const [message, setMessage] = useState("");

  useEffect(() => {
    async function loadInfo() {
      try {
        const payload = await request<UserConnectionInfo>(`/api/admin/users/${encodeURIComponent(user.name)}/connection-info`);
        setInfo(payload);
      } catch (err) {
        setMessage(err instanceof Error ? err.message : "load connection info failed");
      }
    }
    void loadInfo();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [user.name]);

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent size="xl">
        <DialogHeader>
          <DialogTitle>Connection Info: {user.name}</DialogTitle>
        </DialogHeader>
        {message ? <Note variant="error" size="sm">{message}</Note> : null}
        {info ? (
          <Snippet
            wrap
            text={JSON.stringify(info, null, 2)}
          />
        ) : (
          <div className="flex items-center justify-center py-8">
            <LoadingDots>Loading connection info</LoadingDots>
          </div>
        )}
        <DialogFooter>
          <Button size="sm" variant="secondary" onClick={onClose}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ConfirmDeleteModal({
  actionLabel = "Delete",
  error,
  loading,
  message,
  onClose,
  onConfirm,
  title
}: {
  actionLabel?: string;
  error?: string;
  loading: boolean;
  message: string;
  onClose: () => void;
  onConfirm: () => void;
  title: string;
}) {
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent size="md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <Note variant="warning" size="sm">
            This will disable the resource and keep historical records.
          </Note>
          <div className="rounded-md border border-gray-alpha-400 bg-background-200 px-3 py-2 font-mono text-xs text-gray-1000">
            {message}
          </div>
          {error ? <Note variant="error" size="sm">{error}</Note> : null}
        </div>
        <DialogFooter>
          <Button size="sm" variant="secondary" disabled={loading} onClick={onClose}>
            Cancel
          </Button>
          <Button size="sm" variant="error" loading={loading} prefix={<Trash2 size={14} />} onClick={onConfirm}>
            {actionLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function GrantAccessModal({
  existingAccesses,
  onClose,
  onGranted,
  request,
  user
}: {
  existingAccesses: AdminProxyAccess[];
  onClose: () => void;
  onGranted: () => Promise<void>;
  request: Requester;
  user: AdminUser;
}) {
  const [proxies, setProxies] = useState<AdminProxy[]>([]);
  const [selectedProxyID, setSelectedProxyID] = useState("");
  const [message, setMessage] = useState("");
  const [saving, setSaving] = useState(false);
  const existingKeys = new Set(
    existingAccesses
      .filter((access) => access.enabled)
      .map((access) => `${access.node_name}/${access.proxy_name}`)
  );
  const availableProxies = proxies.filter((proxy) => proxy.enabled && !existingKeys.has(`${proxy.node_name}/${proxy.name}`));
  const selectedProxy = availableProxies.find((proxy) => proxy.id === selectedProxyID) || availableProxies[0];

  useEffect(() => {
    async function loadProxies() {
      try {
        const rows = await request<AdminProxy[]>("/api/admin/proxies");
        setProxies(rows);
      } catch (err) {
        setMessage(err instanceof Error ? err.message : "load proxies failed");
      }
    }
    void loadProxies();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function grantAccess() {
    if (!selectedProxy) {
      setMessage("No available proxy.");
      return;
    }
    setSaving(true);
    setMessage("");
    try {
      await request<AdminProxyAccess>(`/api/admin/users/${encodeURIComponent(user.name)}/proxies`, {
        method: "POST",
        body: JSON.stringify({
          node_name: selectedProxy.node_name,
          proxy_name: selectedProxy.name
        })
      });
      await onGranted();
      onClose();
    } catch (err) {
      setMessage(err instanceof Error ? err.message : "grant access failed");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent size="md">
        <DialogHeader>
          <DialogTitle>Grant Access: {user.name}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <FieldRow label="Proxy" required>
            <Select
              value={selectedProxy?.id || ""}
              onValueChange={setSelectedProxyID}
              disabled={availableProxies.length === 0}
            >
              <SelectTrigger>
                <SelectValue placeholder="Select proxy" />
              </SelectTrigger>
              <SelectContent>
                {availableProxies.map((proxy) => (
                  <SelectItem key={proxy.id} value={proxy.id}>
                    {proxy.node_name} / {proxy.name} :{proxy.listen_port}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </FieldRow>
          {availableProxies.length === 0 ? (
            <Note variant="secondary" size="sm">No unassigned enabled proxies are available.</Note>
          ) : null}
          {message ? <Note variant="error" size="sm">{message}</Note> : null}
        </div>
        <DialogFooter>
          <Button
            size="sm"
            disabled={!selectedProxy || saving}
            loading={saving}
            onClick={() => void grantAccess()}
          >
            Grant Access
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function AddUserModal({
  onClose,
  onCreated,
  request
}: {
  onClose: () => void;
  onCreated: () => void;
  request: Requester;
}) {
  const [form, setForm] = useState({
    name: "",
    display_name: "",
    global_quota_bytes: 0,
    traffic_multiplier: 1,
    expire_at: ""
  });
  const [message, setMessage] = useState("");
  const [saving, setSaving] = useState(false);

  async function createUser() {
    setSaving(true);
    setMessage("");
    try {
      await request<AdminUser>("/api/admin/users", {
        method: "POST",
        body: JSON.stringify(form)
      });
      await onCreated();
      onClose();
    } catch (err) {
      setMessage(err instanceof Error ? err.message : "create user failed");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent size="md">
        <DialogHeader>
          <DialogTitle>Add User</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <FieldRow label="Name" required narrow>
            <Input
              autoFocus
              value={form.name}
              onChange={(event) => setForm({ ...form, name: event.target.value })}
            />
          </FieldRow>
          <FieldRow label="Display Name" narrow>
            <Input
              value={form.display_name}
              onChange={(event) => setForm({ ...form, display_name: event.target.value })}
            />
          </FieldRow>
          <FieldRow label="Quota Bytes" hint="0 means unlimited" narrow>
            <Input
              type="number"
              min={0}
              value={form.global_quota_bytes}
              onChange={(event) => setForm({ ...form, global_quota_bytes: Number(event.target.value) })}
            />
          </FieldRow>
          <FieldRow label="Traffic Multiplier" narrow>
            <Input
              type="number"
              min={0}
              step={0.01}
              value={form.traffic_multiplier}
              onChange={(event) => setForm({ ...form, traffic_multiplier: Number(event.target.value) })}
            />
          </FieldRow>
          <FieldRow label="Expires" hint="Optional ISO date, e.g. 2026-12-31" narrow>
            <Input
              value={form.expire_at}
              onChange={(event) => setForm({ ...form, expire_at: event.target.value })}
            />
          </FieldRow>
          {message ? <Note variant="error" size="sm">{message}</Note> : null}
        </div>
        <DialogFooter>
          <Button
            size="sm"
            disabled={!form.name.trim() || saving}
            loading={saving}
            onClick={() => void createUser()}
          >
            Create User
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function TrafficPage({ rows }: { rows: TrafficRow[] }) {
  return (
    <Panel title="User Traffic">
      <UserTrafficTable rows={rows} />
    </Panel>
  );
}

type UserTrafficTotals = {
  userName: string;
  uplinkRaw: number;
  uplinkBillable: number;
  downlinkRaw: number;
  downlinkBillable: number;
  totalBillable: number;
};

function groupTrafficRows(rows: TrafficRow[]): UserTrafficTotals[] {
  const byUser = new Map<string, UserTrafficTotals>();
  for (const row of rows) {
    const userName = row.user_name || "unknown";
    const totals = byUser.get(userName) ?? {
      userName,
      uplinkRaw: 0,
      uplinkBillable: 0,
      downlinkRaw: 0,
      downlinkBillable: 0,
      totalBillable: 0
    };
    const direction = row.direction.toLowerCase();
    if (direction === "uplink") {
      totals.uplinkRaw += row.raw_bytes;
      totals.uplinkBillable += row.billable_bytes;
    } else if (direction === "downlink") {
      totals.downlinkRaw += row.raw_bytes;
      totals.downlinkBillable += row.billable_bytes;
    } else {
      totals.downlinkRaw += row.raw_bytes;
      totals.downlinkBillable += row.billable_bytes;
    }
    totals.totalBillable += row.billable_bytes;
    byUser.set(userName, totals);
  }
  return Array.from(byUser.values()).sort((left, right) => right.totalBillable - left.totalBillable);
}

function UserTrafficTable({ rows, compact = false, limit }: { rows: TrafficRow[]; compact?: boolean; limit?: number }) {
  const users = groupTrafficRows(rows);
  const visibleUsers = typeof limit === "number" ? users.slice(0, limit) : users;
  if (visibleUsers.length === 0) {
    return <EmptyState text="No traffic samples have been reported." />;
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>User</TableHead>
          <TableHead>Upload</TableHead>
          <TableHead>Download</TableHead>
          {!compact ? <TableHead>Total Billable</TableHead> : null}
        </TableRow>
      </TableHeader>
      <TableBody>
        {visibleUsers.map((row) => (
          <TableRow key={row.userName}>
            <TableCell className="font-medium text-gray-1000">{row.userName}</TableCell>
            <TableCell>
              <TrafficAmount
                billableBytes={row.uplinkBillable}
                compact={compact}
                icon={<Upload size={14} />}
                rawBytes={row.uplinkRaw}
              />
            </TableCell>
            <TableCell>
              <TrafficAmount
                billableBytes={row.downlinkBillable}
                compact={compact}
                icon={<Download size={14} />}
                rawBytes={row.downlinkRaw}
              />
            </TableCell>
            {!compact ? <TableCell className="text-gray-900">{formatBytes(row.totalBillable)}</TableCell> : null}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function TrafficAmount({
  billableBytes,
  compact,
  icon,
  rawBytes
}: {
  billableBytes: number;
  compact: boolean;
  icon: React.ReactNode;
  rawBytes: number;
}) {
  return (
    <span className="inline-flex min-w-[116px] items-center gap-2 text-gray-900">
      <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-gray-alpha-200 text-gray-900">
        {icon}
      </span>
      <span className="flex min-w-0 flex-col">
        <span className="font-medium text-gray-1000">{formatBytes(billableBytes)}</span>
        {!compact ? <span className="text-xs text-gray-700">raw {formatBytes(rawBytes)}</span> : null}
      </span>
    </span>
  );
}

const networkEventAllValue = "__boxfleet_all__";
const networkEventPageSizes = ["25", "50", "100", "250", "500"] as const;

const networkEventFiltersSchema = z.object({
  node: z.string(),
  user: z.string(),
  limit: z.enum(networkEventPageSizes),
  dateRange: z.object({
    from: z.date().optional(),
    to: z.date().optional()
  }).optional(),
  startTime: z.string().refine(isTimeInputValue, "Use HH:mm"),
  endTime: z.string().refine(isTimeInputValue, "Use HH:mm")
});

type NetworkEventFilters = z.infer<typeof networkEventFiltersSchema>;

function networkEventDefaultFilters(limit = 100): NetworkEventFilters {
  return {
    node: networkEventAllValue,
    user: networkEventAllValue,
    limit: networkEventPageSize(limit),
    dateRange: undefined,
    startTime: "00:00",
    endTime: "23:59"
  };
}

function networkEventPageSize(limit: number): NetworkEventFilters["limit"] {
  const value = String(limit);
  return networkEventPageSizes.includes(value as NetworkEventFilters["limit"])
    ? (value as NetworkEventFilters["limit"])
    : "100";
}

function isTimeInputValue(value: string): boolean {
  return value === "" || /^([01]\d|2[0-3]):[0-5]\d$/.test(value);
}

export function NetworkEventsPage({
  nodes,
  request,
  users
}: {
  nodes: AdminNode[];
  request: Requester;
  users: AdminUser[];
}) {
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const searchParamKey = searchParams.toString();
  const { filters: appliedFilters, offset } = useMemo(
    () => networkEventStateFromSearchParams(searchParams),
    [searchParamKey]
  );
  const form = useForm<NetworkEventFilters>({
    resolver: zodResolver(networkEventFiltersSchema),
    defaultValues: appliedFilters
  });
  const [retentionInput, setRetentionInput] = useState("90");
  const [settingsMessage, setSettingsMessage] = useState("");
  const [savingSettings, setSavingSettings] = useState(false);

  const queryString = networkEventQueryString(appliedFilters, offset);
  const pageQuery = useQuery({
    queryKey: ["admin", "network-events", "page", queryString],
    queryFn: () => request<NetworkEventsResponse>(`/api/admin/network-events?${queryString}`),
    placeholderData: (previous) => previous
  });
  const settingsQuery = useQuery({
    queryKey: ["admin", "settings"],
    queryFn: () => request<AdminSettings>("/api/admin/settings")
  });
  const response = pageQuery.data ?? { events: [], total: 0, limit: Number(appliedFilters.limit), offset };
  const loading = pageQuery.isFetching;
  const dateRange = form.watch("dateRange");

  useEffect(() => {
    form.reset(appliedFilters);
  }, [appliedFilters, form]);

  useEffect(() => {
    if (settingsQuery.data) {
      setRetentionInput(String(settingsQuery.data.network_event_retention_days));
    }
  }, [settingsQuery.data]);

  function applyFilters(values: NetworkEventFilters) {
    setSearchParams(networkEventURLSearchParams(values, 0));
  }

  function clearFilters() {
    form.reset(networkEventDefaultFilters());
    setSearchParams(new URLSearchParams());
  }

  function goToOffset(nextOffset: number) {
    setSearchParams(networkEventURLSearchParams(appliedFilters, nextOffset));
  }

  async function saveSettings() {
    const days = Number(retentionInput);
    setSettingsMessage("");
    if (!Number.isInteger(days) || days < 1 || days > 3650) {
      setSettingsMessage("Retention days must be between 1 and 3650.");
      return;
    }
    setSavingSettings(true);
    try {
      const updated = await request<AdminSettings>("/api/admin/settings", {
        method: "PATCH",
        body: JSON.stringify({ network_event_retention_days: days })
      });
      setRetentionInput(String(updated.network_event_retention_days));
      setSettingsMessage("Saved.");
      await queryClient.invalidateQueries({ queryKey: ["admin", "settings"] });
    } catch (err) {
      setSettingsMessage(err instanceof Error ? err.message : "save settings failed");
    } finally {
      setSavingSettings(false);
    }
  }

  const pageStart = response.total === 0 ? 0 : response.offset + 1;
  const pageEnd = Math.min(response.offset + response.events.length, response.total);
  const canPrev = response.offset > 0;
  const canNext = response.offset + response.limit < response.total;

  return (
    <Panel
      title="Network Events"
      action={
        <div className="flex items-center gap-2">
          <span className="text-xs text-gray-700">
            {pageStart}-{pageEnd} / {response.total}
          </span>
          <Button size="sm" variant="secondary" disabled={loading} onClick={() => void pageQuery.refetch()}>
            Refresh
          </Button>
        </div>
      }
    >
      <form className="border-b border-gray-alpha-400 p-4" onSubmit={form.handleSubmit(applyFilters)}>
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-6">
          <Label className="flex flex-col gap-2 text-xs text-gray-700">
            Node
            <Controller
              control={form.control}
              name="node"
              render={({ field }) => (
                <Select value={field.value} onValueChange={field.onChange}>
                  <SelectTrigger>
                    <SelectValue placeholder="All nodes" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={networkEventAllValue}>All nodes</SelectItem>
                    {nodes.map((node) => (
                      <SelectItem key={node.name} value={node.name}>{node.name}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />
          </Label>
          <Label className="flex flex-col gap-2 text-xs text-gray-700">
            User
            <Controller
              control={form.control}
              name="user"
              render={({ field }) => (
                <Select value={field.value} onValueChange={field.onChange}>
                  <SelectTrigger>
                    <SelectValue placeholder="All users" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={networkEventAllValue}>All users</SelectItem>
                    {users.map((user) => (
                      <SelectItem key={user.name} value={user.name}>{user.name}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />
          </Label>
          <Controller
            control={form.control}
            name="dateRange"
            render={({ field }) => (
              <Label className="flex flex-col gap-2 text-xs text-gray-700">
                Date Range
                <Popover>
                  <PopoverTrigger asChild>
                    <Button type="button" variant="secondary" className="h-10 justify-start px-3 font-normal">
                      {formatDateRangeLabel(field.value)}
                    </Button>
                  </PopoverTrigger>
                  <PopoverContent align="start" className="w-auto">
                    <Calendar
                      mode="range"
                      numberOfMonths={2}
                      selected={field.value as DateRange | undefined}
                      onSelect={field.onChange}
                    />
                  </PopoverContent>
                </Popover>
              </Label>
            )}
          />
          <Input
            type="time"
            label="Start Time"
            disabled={!dateRange?.from}
            {...form.register("startTime")}
          />
          <Input
            type="time"
            label="End Time"
            disabled={!dateRange?.from}
            {...form.register("endTime")}
          />
          <Label className="flex flex-col gap-2 text-xs text-gray-700">
            Page Size
            <Controller
              control={form.control}
              name="limit"
              render={({ field }) => (
                <Select value={field.value} onValueChange={field.onChange}>
                  <SelectTrigger>
                    <SelectValue placeholder="Page size" />
                  </SelectTrigger>
                  <SelectContent>
                    {networkEventPageSizes.map((value) => (
                      <SelectItem key={value} value={value}>{value}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />
          </Label>
          <div className="flex items-end gap-2">
            <Button type="submit" size="sm" disabled={loading}>Apply</Button>
            <Button type="button" size="sm" variant="secondary" disabled={loading} onClick={clearFilters}>Clear</Button>
          </div>
        </div>
        {pageQuery.error ? (
          <div className="mt-3">
            <Note variant="error" size="sm">{pageQuery.error instanceof Error ? pageQuery.error.message : "load network events failed"}</Note>
          </div>
        ) : null}
        {form.formState.errors.startTime || form.formState.errors.endTime ? (
          <div className="mt-3">
            <Note variant="error" size="sm">Time must be HH:mm.</Note>
          </div>
        ) : null}
        <div className="mt-3 flex flex-wrap items-end gap-2">
          <Input
            type="number"
            min={1}
            max={3650}
            size="sm"
            label="Retention Days"
            value={retentionInput}
            onChange={(event) => setRetentionInput(event.target.value)}
            containerClassName="w-[120px]"
          />
          <Button
            type="button"
            size="sm"
            variant="secondary"
            loading={savingSettings}
            disabled={settingsQuery.isLoading}
            onClick={() => void saveSettings()}
          >
            Save
          </Button>
          {settingsQuery.isFetching && !savingSettings ? <Spinner size={14} /> : null}
          {settingsMessage ? (
            <span className="pb-1 text-xs text-gray-700">{settingsMessage}</span>
          ) : null}
        </div>
        {settingsQuery.error ? (
          <div className="mt-3">
            <Note variant="error" size="sm">{settingsQuery.error instanceof Error ? settingsQuery.error.message : "load settings failed"}</Note>
          </div>
        ) : null}
      </form>
      <NetworkEventsDataTable events={response.events} />
      <div className="flex items-center justify-between border-t border-gray-alpha-400 px-4 py-3">
        <span className="text-xs text-gray-700">
          {response.total === 0 ? "No matching events" : `${pageStart}-${pageEnd} of ${response.total}`}
        </span>
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            variant="secondary"
            disabled={!canPrev || loading}
            onClick={() => goToOffset(Math.max(0, response.offset - response.limit))}
          >
            Previous
          </Button>
          <Button
            size="sm"
            variant="secondary"
            disabled={!canNext || loading}
            onClick={() => goToOffset(response.offset + response.limit)}
          >
            Next
          </Button>
        </div>
      </div>
    </Panel>
  );
}

function networkEventQueryString(filters: NetworkEventFilters, offset: number): string {
  return networkEventSearchParams(filters, offset, true).toString();
}

function networkEventURLSearchParams(filters: NetworkEventFilters, offset: number): URLSearchParams {
  return networkEventSearchParams(filters, offset, false);
}

function networkEventSearchParams(filters: NetworkEventFilters, offset: number, includeDefaults: boolean): URLSearchParams {
  const params = new URLSearchParams();
  if (includeDefaults || filters.limit !== "100") {
    params.set("limit", filters.limit);
  }
  if (includeDefaults || offset > 0) {
    params.set("offset", String(Math.max(0, offset)));
  }
  if (filters.node !== networkEventAllValue) {
    params.set("node", filters.node);
  }
  if (filters.user !== networkEventAllValue) {
    params.set("user", filters.user);
  }
  const range = filters.dateRange;
  if (range?.from) {
    params.set("start", combineLocalDateAndTime(range.from, filters.startTime, "start").toISOString());
    params.set("end", combineLocalDateAndTime(range.to ?? range.from, filters.endTime, "end").toISOString());
  }
  return params;
}

function networkEventStateFromSearchParams(params: URLSearchParams): { filters: NetworkEventFilters; offset: number } {
  const defaults = networkEventDefaultFilters();
  const startDate = parseNetworkEventParamDate(params.get("start"));
  const endDate = parseNetworkEventParamDate(params.get("end"));
  let dateRange: DateRange | undefined;
  let startTime = defaults.startTime;
  let endTime = defaults.endTime;
  if (startDate) {
    dateRange = { from: startDate, to: endDate ?? startDate };
    startTime = format(startDate, "HH:mm");
    endTime = format(endDate ?? startDate, "HH:mm");
  } else if (endDate) {
    dateRange = { from: endDate, to: endDate };
    endTime = format(endDate, "HH:mm");
  }
  return {
    filters: {
      node: params.get("node") || defaults.node,
      user: params.get("user") || defaults.user,
      limit: networkEventPageSize(Number(params.get("limit"))),
      dateRange,
      startTime,
      endTime
    },
    offset: networkEventOffset(params.get("offset"))
  };
}

function networkEventOffset(value: string | null): number {
  const offset = Number(value);
  return Number.isInteger(offset) && offset > 0 ? offset : 0;
}

function parseNetworkEventParamDate(value: string | null): Date | undefined {
  if (!value) {
    return undefined;
  }
  const date = new Date(value);
  return isValid(date) ? date : undefined;
}

function combineLocalDateAndTime(date: Date, value: string, edge: "start" | "end"): Date {
  const [hourRaw, minuteRaw] = value.split(":");
  const hour = Number(hourRaw);
  const minute = Number(minuteRaw);
  const next = new Date(date);
  if (Number.isFinite(hour) && Number.isFinite(minute)) {
    next.setHours(hour, minute, edge === "start" ? 0 : 59, edge === "start" ? 0 : 999);
  } else if (edge === "start") {
    next.setHours(0, 0, 0, 0);
  } else {
    next.setHours(23, 59, 59, 999);
  }
  return next;
}

function formatDateRangeLabel(range?: NetworkEventFilters["dateRange"]): string {
  if (!range?.from) {
    return "Any date";
  }
  if (!range.to) {
    return format(range.from, "yyyy-MM-dd");
  }
  return `${format(range.from, "yyyy-MM-dd")} - ${format(range.to, "yyyy-MM-dd")}`;
}

function NetworkEventsDataTable({ events }: { events: NetworkEvent[] }) {
  const columns: ColumnDef<NetworkEvent>[] = [
    {
      accessorKey: "node_name",
      header: "Node",
      cell: ({ row }) => <span className="text-gray-900">{row.original.node_name || "—"}</span>
    },
    {
      accessorKey: "user_name",
      header: "User",
      cell: ({ row }) => <span className="font-medium text-gray-1000">{row.original.user_name || row.original.auth_name || "—"}</span>
    },
    {
      accessorKey: "action",
      header: "Action",
      cell: ({ row }) => <StatusBadge value={row.original.action || "event"} />
    },
    {
      id: "target",
      header: "Target",
      cell: ({ row }) => (
        <span className="text-gray-900">
          {row.original.target_host || "—"}
          {row.original.target_port ? `:${row.original.target_port}` : ""}
        </span>
      )
    },
    {
      accessorKey: "source_ip",
      header: "Source",
      cell: ({ row }) => <span className="text-gray-900">{row.original.source_ip || "—"}</span>
    },
    {
      id: "started",
      header: "Started",
      cell: ({ row }) => <span className="text-gray-900">{formatLocalDateTime(row.original.window_start) || "—"}</span>
    },
    {
      id: "duration",
      header: "Duration",
      cell: ({ row }) => <span className="text-gray-900">{formatEventDuration(row.original.window_start, row.original.window_end) || "—"}</span>
    },
    {
      accessorKey: "count",
      header: "Count",
      cell: ({ row }) => <span className="text-gray-900">{row.original.count || 1}</span>
    },
    {
      id: "observed",
      header: "Observed",
      cell: ({ row }) => (
        <span className="text-gray-900">
          {formatLocalDateTime(row.original.window_end || row.original.created_at || row.original.window_start) || "—"}
        </span>
      )
    },
    {
      id: "raw",
      header: "Raw",
      cell: ({ row }) => (
        <details className="cursor-pointer">
          <summary className="text-blue-700">View</summary>
          <pre className="mt-2 max-w-[520px] overflow-auto rounded-md bg-gray-1000 p-3 text-xs text-gray-100">
            {row.original.raw_message}
          </pre>
        </details>
      )
    }
  ];
  const table = useReactTable({
    data: events,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getRowId: (row, index) => `${row.created_at}-${row.auth_name}-${row.target_host}-${index}`
  });

  if (events.length === 0) {
    return <EmptyState text="No network events have been uploaded." />;
  }
  return (
    <Table>
      <TableHeader>
        {table.getHeaderGroups().map((headerGroup) => (
          <TableRow key={headerGroup.id}>
            {headerGroup.headers.map((header) => (
              <TableHead key={header.id}>
                {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
              </TableHead>
            ))}
          </TableRow>
        ))}
      </TableHeader>
      <TableBody>
        {table.getRowModel().rows.map((row) => (
          <TableRow key={row.id}>
            {row.getVisibleCells().map((cell) => (
              <TableCell key={cell.id}>
                {flexRender(cell.column.columnDef.cell, cell.getContext())}
              </TableCell>
            ))}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function formatLocalDateTime(value: string): string {
  if (!value) {
    return "";
  }
  const date = new Date(value);
  if (!isValid(date)) {
    return value;
  }
  return format(date, "yyyy-MM-dd HH:mm:ss");
}

function formatEventDuration(start: string, end: string): string {
  const startDate = new Date(start);
  const endDate = new Date(end);
  if (!isValid(startDate) || !isValid(endDate)) {
    return "";
  }
  const ms = differenceInMilliseconds(endDate, startDate);
  if (ms < 0) {
    return "";
  }
  if (ms < 1000) {
    return `${ms} ms`;
  }
  const seconds = ms / 1000;
  if (seconds < 60) {
    return `${seconds < 10 ? seconds.toFixed(2) : seconds.toFixed(1)} s`;
  }
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = Math.round(seconds % 60);
  if (minutes < 60) {
    return `${minutes}m ${remainingSeconds}s`;
  }
  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;
  return `${hours}h ${remainingMinutes}m`;
}

export function SystemLogsPage({ response }: { response: SystemLogsResponse }) {
  return (
    <Panel title="System Logs">
      {response.logs.length === 0 ? (
        <EmptyState text={response.note || "No system logs are stored yet."} />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Node</TableHead>
              <TableHead>Service</TableHead>
              <TableHead>Level</TableHead>
              <TableHead>Observed</TableHead>
              <TableHead>Message</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {response.logs.map((log, index) => (
              <TableRow key={`${log.node}-${log.service}-${log.observed_at}-${index}`}>
                <TableCell className="text-gray-1000">{log.node}</TableCell>
                <TableCell className="text-gray-900">{log.service}</TableCell>
                <TableCell className="text-gray-900">{log.level || "—"}</TableCell>
                <TableCell className="text-gray-900">{formatTime(log.observed_at)}</TableCell>
                <TableCell className="font-mono text-xs text-gray-900">
                  <AnsiText text={log.message} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </Panel>
  );
}

function AnsiText({ text }: { text: string }) {
  return (
    <>
      {Anser.ansiToJson(text, { remove_empty: true }).map((part, index) => {
        const style: React.CSSProperties = {};
        if (part.fg) {
          style.color = `rgb(${part.fg})`;
        }
        if (part.bg) {
          style.backgroundColor = `rgb(${part.bg})`;
        }
        if (part.decorations?.includes("bold")) {
          style.fontWeight = 600;
        }
        if (part.decorations?.includes("italic")) {
          style.fontStyle = "italic";
        }
        if (part.decorations?.includes("underline")) {
          style.textDecoration = "underline";
        }
        return (
          <span key={`${index}-${part.content}`} style={style}>
            {part.content}
          </span>
        );
      })}
    </>
  );
}
