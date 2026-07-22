import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowRightIcon,
  CheckCircleIcon,
  ClockIcon,
  DownloadSimpleIcon,
  WarningCircleIcon
} from "@phosphor-icons/react";
import { Banner, Button, Checkbox, Dialog, Meter, Select, Table } from "@cloudflare/kumo";

import { useAdminMutation } from "@/admin/use-admin-mutation";
import type { AdminRequest } from "@/publish/publish-status";
import type {
  AdminNode,
  AdminRelease,
  NodeOperation,
  NodeOperationDetail,
  NodeOperationEvent,
  NodeUpdateCampaignDetail
} from "@/types";

type UpdateComponent = "agent" | "sing_box";

const terminalOperationStatuses = new Set(["succeeded", "failed", "cancelled", "expired"]);
const terminalCampaignStatuses = new Set(["succeeded", "cancelled"]);

function idempotencyKey(prefix: string) {
  const suffix = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random()}`;
  return `${prefix}:${suffix}`;
}

function phaseLabel(value: string) {
  const labels: Record<string, string> = {
    queued: "Queued",
    claimed: "Claimed",
    starting: "Starting",
    downloading: "Downloading",
    verifying: "Verifying",
    installing_agent: "Installing agent",
    restarting_agent: "Restarting agent",
    agent_confirmed: "Agent confirmed",
    installing_sing_box: "Installing sing-box",
    completed: "Completed",
    failed: "Failed",
    cancelled: "Cancelled"
  };
  return labels[value] ?? value.replaceAll("_", " ");
}

function operationStatusClass(status: string) {
  if (status === "succeeded") return "text-kumo-success";
  if (status === "failed" || status === "expired") return "text-kumo-danger";
  if (status === "cancelled") return "text-kumo-warning";
  return "text-kumo-info";
}

function latestDownloadProgress(events: NodeOperationEvent[]) {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const details = events[index]?.details;
    const complete = Number(details?.bytes_complete);
    const size = Number(details?.size);
    if (Number.isFinite(complete) && Number.isFinite(size) && size > 0) {
      return { complete, size };
    }
  }
  return null;
}

function OperationProgress({ detail }: { detail: NodeOperationDetail }) {
  const { operation, events } = detail;
  const progress = latestDownloadProgress(events);
  return (
    <div className="flex flex-col gap-4">
      <div className="rounded-lg border border-kumo-line bg-kumo-recessed p-4">
        <div className="flex items-center justify-between gap-3">
          <span className="text-sm font-medium text-kumo-default">{phaseLabel(operation.phase)}</span>
          <span className={`text-sm font-medium ${operationStatusClass(operation.status)}`}>{operation.status}</span>
        </div>
        <p className="mt-1 text-sm text-kumo-subtle">
          Attempt {operation.attempt || 0} · updated {new Date(operation.updated_at).toLocaleString()}
        </p>
      </div>
      {progress ? (
        <Meter
          label="Download"
          value={progress.complete}
          max={progress.size}
          customValue={`${Math.round(progress.complete / 1024 / 1024)} / ${Math.round(progress.size / 1024 / 1024)} MiB`}
        />
      ) : null}
      {operation.error ? <Banner variant="error" title="Update failed" description={operation.error} /> : null}
      {events.length > 0 ? (
        <div className="max-h-56 overflow-y-auto rounded-lg border border-kumo-line">
          <Table>
            <Table.Header variant="compact">
              <Table.Row>
                <Table.Head>Stage</Table.Head>
                <Table.Head>Message</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {events.map((event) => (
                <Table.Row key={event.id}>
                  <Table.Cell>
                    <span className={`whitespace-nowrap text-sm font-medium ${operationStatusClass(event.status)}`}>
                      {phaseLabel(event.phase)}
                    </span>
                  </Table.Cell>
                  <Table.Cell>
                    <span className="text-sm text-kumo-subtle">{event.error || event.message || "—"}</span>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </div>
      ) : null}
    </div>
  );
}

export function NodeUpdateDialog({
  request,
  node,
  release,
  initialComponents,
  initialOperation,
  onClose
}: {
  request: AdminRequest;
  node: AdminNode;
  release: AdminRelease;
  initialComponents?: UpdateComponent[];
  initialOperation?: NodeOperation;
  onClose: () => void;
}) {
  const agentOutdated = !versionsEqual(node.agent_version, release.boxfleet_version);
  const singBoxOutdated = !versionsEqual(node.sing_box_version, release.sing_box_version);
  const agentSupported = node.capabilities?.includes("update.agent.v1") ?? false;
  const singBoxSupported = node.capabilities?.includes("update.sing_box.v1") ?? false;
  const defaults = initialComponents ?? [
    ...(agentOutdated && agentSupported ? (["agent"] as UpdateComponent[]) : []),
    ...(singBoxOutdated && singBoxSupported ? (["sing_box"] as UpdateComponent[]) : [])
  ];
  const [components, setComponents] = useState<UpdateComponent[]>(defaults);
  const [operationID, setOperationID] = useState(initialOperation?.id ?? "");

  const operationQuery = useQuery({
    queryKey: ["admin", "node-operation", node.name, operationID],
    queryFn: () =>
      request<NodeOperationDetail>(
        `/api/admin/nodes/${encodeURIComponent(node.name)}/operations/${encodeURIComponent(operationID)}`
      ),
    enabled: operationID !== "",
    refetchInterval: (query) =>
      terminalOperationStatuses.has(query.state.data?.operation.status ?? "") ? false : 2000
  });
  const mutation = useAdminMutation<UpdateComponent[], NodeOperation>(
    request,
    (req, selected) =>
      req(`/api/admin/nodes/${encodeURIComponent(node.name)}/updates`, {
        method: "POST",
        body: JSON.stringify({
          components: selected,
          idempotency_key: idempotencyKey(`node-update:${node.name}`)
        })
      }),
    { onSuccess: (operation) => setOperationID(operation.id) }
  );
  const cancelMutation = useAdminMutation<void, NodeOperation>(request, (req) =>
    req(`/api/admin/nodes/${encodeURIComponent(node.name)}/operations/${encodeURIComponent(operationID)}/cancel`, {
      method: "POST"
    })
  );

  const detail = operationQuery.data ??
    (initialOperation && operationID === initialOperation.id ? { operation: initialOperation, events: [] } : null);
  const terminal = detail ? terminalOperationStatuses.has(detail.operation.status) : false;
  const canSubmit = components.length > 0 && !mutation.isPending;

  function toggle(component: UpdateComponent, checked: boolean) {
    setComponents((current) =>
      checked ? [...new Set([...current, component])] : current.filter((value) => value !== component)
    );
  }

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog size="base" className="max-h-[calc(100vh-2rem)] overflow-y-auto p-4 sm:w-[32rem] sm:p-6 lg:w-[40rem]">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">Update {node.name}</Dialog.Title>
        <Dialog.Description className="mb-5 text-kumo-subtle">
          The agent will claim this operation over HTTPS. No SSH session is used.
        </Dialog.Description>

        {mutation.isError ? <Banner variant="error" title={mutation.error.message} className="mb-4" /> : null}
        {cancelMutation.isError ? <Banner variant="error" title={cancelMutation.error.message} className="mb-4" /> : null}

        {detail ? (
          <OperationProgress detail={detail} />
        ) : (
          <div className="flex flex-col gap-4">
            <Checkbox
              checked={components.includes("agent")}
              disabled={!agentOutdated || !agentSupported}
              onCheckedChange={(checked) => toggle("agent", checked)}
              label={
                <span className="inline-flex items-center gap-2">
                  Agent <span className="text-kumo-subtle">{node.agent_version || "unknown"}</span>
                  <ArrowRightIcon className="size-3.5 text-kumo-inactive" /> {release.boxfleet_version}
                </span>
              }
            />
            <Checkbox
              checked={components.includes("sing_box")}
              disabled={!singBoxOutdated || !singBoxSupported}
              onCheckedChange={(checked) => toggle("sing_box", checked)}
              label={
                <span className="inline-flex items-center gap-2">
                  sing-box <span className="text-kumo-subtle">{node.sing_box_version || "unknown"}</span>
                  <ArrowRightIcon className="size-3.5 text-kumo-inactive" /> {release.sing_box_version}
                </span>
              }
            />
            <Banner
              variant="secondary"
              icon={<ClockIcon weight="fill" />}
              title="Safe to queue while offline"
              description="The operation remains durable until this node reconnects."
            />
            {(agentOutdated && !agentSupported) || (singBoxOutdated && !singBoxSupported) ? (
              <Banner
                variant="alert"
                title="Manual component upgrade required"
                description="This agent did not advertise every updater capability required by the outdated component."
              />
            ) : null}
          </div>
        )}

        <div className="mt-6 flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            {terminal ? "Done" : "Close"}
          </Button>
          {!detail ? (
            <Button
              icon={DownloadSimpleIcon}
              loading={mutation.isPending}
              disabled={!canSubmit}
              onClick={() => mutation.mutate(components)}
            >
              Update
            </Button>
          ) : !terminal ? (
            <Button
              variant="secondary-destructive"
              loading={cancelMutation.isPending}
              disabled={detail.operation.cancel_requested}
              onClick={() => cancelMutation.mutate()}
            >
              {detail.operation.cancel_requested ? "Cancellation requested" : "Cancel safely"}
            </Button>
          ) : detail.operation.status === "failed" || detail.operation.status === "expired" ? (
            <Button
              icon={DownloadSimpleIcon}
              loading={mutation.isPending}
              onClick={() => {
                setOperationID("");
                mutation.mutate(components);
              }}
            >
              Retry
            </Button>
          ) : null}
        </div>
      </Dialog>
    </Dialog.Root>
  );
}

export function UpdateAllDialog({
  request,
  release,
  initialCampaign,
  onClose
}: {
  request: AdminRequest;
  release: AdminRelease;
  initialCampaign?: NodeUpdateCampaignDetail;
  onClose: () => void;
}) {
  const [components, setComponents] = useState<UpdateComponent[]>(["agent", "sing_box"]);
  const [batchSize, setBatchSize] = useState("2");
  const [campaignID, setCampaignID] = useState(initialCampaign?.campaign.id ?? "");
  const campaignQuery = useQuery({
    queryKey: ["admin", "node-update-campaign", campaignID],
    queryFn: () => request<NodeUpdateCampaignDetail>(`/api/admin/node-update-campaigns/${encodeURIComponent(campaignID)}`),
    enabled: campaignID !== "",
    refetchInterval: (query) =>
      terminalCampaignStatuses.has(query.state.data?.campaign.status ?? "") ? false : 2000
  });
  const mutation = useAdminMutation<void, NodeUpdateCampaignDetail>(
    request,
    (req) =>
      req("/api/admin/node-updates/bulk", {
        method: "POST",
        body: JSON.stringify({
          components,
          batch_size: Number(batchSize),
          idempotency_key: idempotencyKey("update-all")
        })
      }),
    { onSuccess: (detail) => setCampaignID(detail.campaign.id) }
  );
  const cancelMutation = useAdminMutation<void, NodeUpdateCampaignDetail>(request, (req) =>
    req(`/api/admin/node-update-campaigns/${encodeURIComponent(campaignID)}/cancel`, { method: "POST" })
  );
  const resumeMutation = useAdminMutation<void, NodeUpdateCampaignDetail>(request, (req) =>
    req(`/api/admin/node-update-campaigns/${encodeURIComponent(campaignID)}/resume`, { method: "POST" })
  );
  const detail = campaignQuery.data ?? initialCampaign ?? null;
  const completed = detail?.members.filter((member) => member.status === "succeeded" || member.status === "skipped").length ?? 0;
  const terminal = detail ? terminalCampaignStatuses.has(detail.campaign.status) : false;
  const grouped = useMemo(() => {
    const groups = new Map<number, NonNullable<typeof detail>["members"]>();
    for (const member of detail?.members ?? []) {
      const current = groups.get(member.batch_number) ?? [];
      current.push(member);
      groups.set(member.batch_number, current);
    }
    return [...groups.entries()];
  }, [detail]);

  function toggle(component: UpdateComponent, checked: boolean) {
    setComponents((current) =>
      checked ? [...new Set([...current, component])] : current.filter((value) => value !== component)
    );
  }

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog size="base" className="max-h-[calc(100vh-2rem)] overflow-y-auto p-4 sm:w-[36rem] sm:p-6 lg:w-[48rem]">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">Update all nodes</Dialog.Title>
        <Dialog.Description className="mb-5 text-kumo-subtle">
          One online node is updated as canary, then the remaining nodes advance in bounded batches.
        </Dialog.Description>

        {mutation.isError ? <Banner variant="error" title={mutation.error.message} className="mb-4" /> : null}
        {cancelMutation.isError ? <Banner variant="error" title={cancelMutation.error.message} className="mb-4" /> : null}
        {resumeMutation.isError ? <Banner variant="error" title={resumeMutation.error.message} className="mb-4" /> : null}

        {detail ? (
          <div className="flex flex-col gap-4">
            {detail.campaign.status === "paused" ? (
              <Banner
                variant="error"
                icon={<WarningCircleIcon weight="fill" />}
                title="Rollout paused"
                description={detail.campaign.error || "A node failed. Later batches were not released."}
              />
            ) : detail.campaign.status === "succeeded" ? (
              <Banner
                icon={<CheckCircleIcon weight="fill" />}
                title="Rollout complete"
                description={`All eligible nodes reached ${release.boxfleet_version}.`}
              />
            ) : null}
            <Meter
              label={`Batch ${Math.max(detail.campaign.current_batch, 0)} · ${detail.campaign.status}`}
              value={completed}
              max={Math.max(detail.members.length, 1)}
              customValue={`${completed} / ${detail.members.length} nodes`}
            />
            <div className="max-h-80 overflow-auto rounded-lg border border-kumo-line">
              <Table>
                <Table.Header variant="compact">
                  <Table.Row>
                    <Table.Head>Batch</Table.Head>
                    <Table.Head>Node</Table.Head>
                    <Table.Head>Components</Table.Head>
                    <Table.Head>Status</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {grouped.flatMap(([batch, members]) =>
                    members.map((member) => (
                      <Table.Row key={member.node_id}>
                        <Table.Cell>{batch === 0 ? "Canary" : batch}</Table.Cell>
                        <Table.Cell>{member.node_name}</Table.Cell>
                        <Table.Cell className="text-kumo-subtle">{member.kind.replace("update.", "").replace("bundle", "agent + sing-box")}</Table.Cell>
                        <Table.Cell>
                          <span className={`font-medium ${operationStatusClass(member.status)}`}>
                            {member.status}
                          </span>
                          {member.error ? <span className="ml-2 text-kumo-danger">{member.error}</span> : null}
                        </Table.Cell>
                      </Table.Row>
                    ))
                  )}
                </Table.Body>
              </Table>
            </div>
          </div>
        ) : (
          <div className="flex flex-col gap-4">
            <Checkbox
              checked={components.includes("agent")}
              onCheckedChange={(checked) => toggle("agent", checked)}
              label={`Agent → ${release.boxfleet_version}`}
            />
            <Checkbox
              checked={components.includes("sing_box")}
              onCheckedChange={(checked) => toggle("sing_box", checked)}
              label={`sing-box → ${release.sing_box_version}`}
            />
            <Select
              label="Batch size"
              hideLabel={false}
              className="w-40"
              value={batchSize}
              onValueChange={(value) => setBatchSize(value ?? "2")}
              items={{ "1": "1 node", "2": "2 nodes" }}
            />
            <Banner
              variant="alert"
              title="Failure stops expansion"
              description="A failed canary or batch pauses every later batch. Pending, deleted, and token-revoked nodes are excluded."
            />
          </div>
        )}

        <div className="mt-6 flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            {terminal ? "Done" : "Close"}
          </Button>
          {!detail ? (
            <Button
              loading={mutation.isPending}
              disabled={components.length === 0}
              onClick={() => mutation.mutate()}
            >
              Start rollout
            </Button>
          ) : !terminal && detail.campaign.status !== "paused" ? (
            <Button variant="secondary-destructive" loading={cancelMutation.isPending} onClick={() => cancelMutation.mutate()}>
              Cancel rollout
            </Button>
          ) : detail.campaign.status === "paused" ? (
            <Button loading={resumeMutation.isPending} onClick={() => resumeMutation.mutate()}>
              Retry failed batch
            </Button>
          ) : null}
        </div>
      </Dialog>
    </Dialog.Root>
  );
}

export function versionsEqual(actual?: string, expected?: string) {
  const normalize = (value?: string) => value?.match(/v?\d+\.\d+\.\d+(?:-[0-9a-z.-]+)?/i)?.[0]?.replace(/^v/i, "") ?? "";
  const a = normalize(actual);
  const b = normalize(expected);
  return a !== "" && b !== "" && a === b;
}
