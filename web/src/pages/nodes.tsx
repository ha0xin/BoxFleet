import { Fragment, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowsClockwiseIcon,
  ArrowRightIcon,
  CheckCircleIcon,
  DotsThreeIcon,
  DownloadSimpleIcon,
  FunnelIcon,
  HardDrivesIcon,
  PencilSimpleIcon,
  PlusIcon,
  ProhibitIcon,
  TrashIcon
} from "@phosphor-icons/react";
import { Banner, Button, DropdownMenu, Input, Table } from "@cloudflare/kumo";

import type { AdminNode, AdminNodesResponse, AdminRelease, NodeUpdateCampaignDetail } from "../types";
import {
  formatNodeVersion,
  formatRelativeTime,
  nodeHealth,
  PageHeader,
  PageTopBar,
} from "./operations-common";
import { useAdminMutation } from "@/admin/use-admin-mutation";
import { useAdminApi } from "@/admin/api";
import { adminKeys, queryString } from "@/admin/query";
import { AdminPagination, SortHead, TableEmpty, TableLoading } from "@/components/admin-table";
import { DeleteNodeDialog, EditNodeDialog, EnrollNodeDialog, ReenrollNodeDialog } from "./node-dialogs";
import type { NodeDialogState } from "./node-dialogs";
import { NodeUpdateDialog, UpdateAllDialog, versionsEqual } from "./node-update-dialogs";

type NodeFilter = "all" | "active" | "disabled" | "degraded" | "deleted";
type NodeSort = "name" | "status" | "public_host" | "last_seen_at" | "sing_box_version";
type SortDirection = "asc" | "desc";
type UpdateDialogState =
  | { mode: "node"; node: AdminNode; components?: Array<"agent" | "sing_box"> }
  | { mode: "all"; campaign?: NodeUpdateCampaignDetail }
  | null;

function nodeStatusFilter(filter: NodeFilter): string | undefined {
  return filter === "all" || filter === "deleted" ? undefined : filter;
}

function nodeTimestamp(node: AdminNode): string {
  return node.latest_heartbeat || node.last_seen_at;
}

function hasCapability(node: AdminNode, capability: string) {
  return node.capabilities?.includes(capability) ?? false;
}

function nodeIsOffline(node: AdminNode) {
  const value = nodeTimestamp(node);
  if (!value) return true;
  return Date.now() - new Date(value).getTime() > 3 * 60 * 1000;
}

function VersionTarget({ current, target }: { current?: string; target: string }) {
	const displayCurrent = current?.match(/v?\d+\.\d+\.\d+(?:-[0-9a-z.-]+)?(?:\+[0-9a-z.-]+)?/i)?.[0] ?? current;
  if (!current || versionsEqual(current, target)) {
    return <span className="whitespace-nowrap text-kumo-subtle" title={current}>{displayCurrent || "n/a"}</span>;
  }
  return (
    <span className="inline-flex items-center gap-1.5 whitespace-nowrap text-sm">
      <span className="text-kumo-subtle" title={current}>{displayCurrent}</span>
      <ArrowRightIcon className="size-3.5 text-kumo-inactive" />
      <span className="font-medium text-kumo-default">{target}</span>
    </span>
  );
}

function nodeUpdateStatus(node: AdminNode, release?: AdminRelease) {
  if (node.active_operation) {
    if (node.active_operation.status === "queued" && nodeIsOffline(node)) {
      return { label: "Queued — waiting for node", className: "text-kumo-warning" };
    }
    return {
      label: node.active_operation.phase.replaceAll("_", " "),
      className: node.active_operation.status === "running" ? "text-kumo-info" : "text-kumo-warning"
    };
  }
  if (!release?.updates_enabled) return { label: "Unavailable", className: "text-kumo-inactive" };
  if (!hasCapability(node, "operations.v1")) {
    return { label: "Manual agent upgrade required", className: "text-kumo-warning" };
  }
  const agentOutdated = !versionsEqual(node.agent_version, release.agent_version);
  const singBoxOutdated = !versionsEqual(node.sing_box_version, release.sing_box_version);
  const canUpdateAgent = agentOutdated && hasCapability(node, "update.agent.v1");
  const canUpdateSingBox = singBoxOutdated && hasCapability(node, "update.sing_box.v1");
  if (canUpdateAgent || canUpdateSingBox) {
    return { label: nodeIsOffline(node) ? "Available · offline" : "Available", className: "text-kumo-info" };
  }
  if (agentOutdated || singBoxOutdated) {
    return { label: "Manual component upgrade required", className: "text-kumo-warning" };
  }
  return { label: "Up to date", className: "text-kumo-success" };
}

export function NodesPage() {
  const { request } = useAdminApi();
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(10);
  const [filter, setFilter] = useState<NodeFilter>("all");
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [sort, setSortValue] = useState<NodeSort>("name");
  const [direction, setDirection] = useState<SortDirection>("asc");
  const [dialog, setDialog] = useState<NodeDialogState>(null);
  const [updateDialog, setUpdateDialog] = useState<UpdateDialogState>(null);

  const toggleStatus = useAdminMutation<AdminNode>(request, (req, node) =>
    req(`/api/admin/nodes/${encodeURIComponent(node.name)}`, {
      method: "PATCH",
      body: JSON.stringify({ status: node.status === "disabled" ? "active" : "disabled" })
    })
  );
  const restore = useAdminMutation<AdminNode>(request, (req, node) =>
    req(`/api/admin/nodes/${encodeURIComponent(node.name)}/restore`, { method: "POST" })
  );

  function setSort(column: NodeSort) {
    setPage(1);
    if (sort === column) {
      setDirection((value) => (value === "asc" ? "desc" : "asc"));
      return;
    }
    setSortValue(column);
    setDirection(column === "last_seen_at" ? "desc" : "asc");
  }

  function setFilterValue(value: NodeFilter) {
    setFilter(value);
    setPage(1);
  }

  function setPageSize(value: number) {
    setPerPage(value);
    setPage(1);
  }

  const offset = (page - 1) * perPage;
  const path =
    "/api/admin/nodes" +
    queryString({
      limit: perPage,
      offset,
      search,
      status: nodeStatusFilter(filter),
      deleted: filter === "deleted" ? "true" : undefined,
      sort,
      direction
    });
  const nodesQuery = useQuery({
    queryKey: adminKeys.nodesPage(perPage, offset, search, filter, sort, direction),
    queryFn: () => request<AdminNodesResponse>(path),
    placeholderData: (previous) => previous,
    refetchInterval: (query) =>
      query.state.data?.nodes.some((node) => node.active_operation) ? 2000 : 15000
  });
  const releaseQuery = useQuery({
    queryKey: adminKeys.release,
    queryFn: () => request<AdminRelease>("/api/admin/release"),
    staleTime: 5 * 60 * 1000
  });
  const campaignQuery = useQuery({
    queryKey: adminKeys.nodeUpdateCampaign("current"),
    queryFn: async () => (await request<NodeUpdateCampaignDetail | undefined>("/api/admin/node-update-campaigns/current")) ?? null,
    refetchInterval: (query) => (query.state.data ? 2000 : 15000)
  });
  const pageData = nodesQuery.data;
  const nodes = pageData?.nodes ?? [];
  const total = pageData?.total ?? 0;
  const error = nodesQuery.error instanceof Error ? nodesQuery.error.message : "Request failed.";
  const release = releaseQuery.data;
  const campaign = campaignQuery.data ?? undefined;

  const lastPage = Math.max(1, Math.ceil(total / perPage));
  if (page > lastPage) setPage(lastPage);

  return (
    <div className="flex min-h-full flex-col bg-kumo-canvas">
      <PageTopBar current="Nodes" />
      <main className="w-full grow bg-kumo-canvas">
        <PageHeader
          title="Nodes"
          description="Operate edge nodes, config versions, heartbeats, and proxy placement."
          actions={
            <Fragment>
              <Button
                variant="secondary"
                icon={DownloadSimpleIcon}
                disabled={!release?.updates_enabled}
                onClick={() => setUpdateDialog({ mode: "all", campaign })}
              >
                Update all
              </Button>
              <Button variant="primary" icon={PlusIcon} onClick={() => setDialog({ mode: "enroll" })}>
                Enroll
              </Button>
            </Fragment>
          }
        />

        <div className="mx-auto flex w-full max-w-[1400px] flex-col gap-4 px-6 pb-8 md:px-8 lg:px-10">
          {campaign ? (
            <Banner
              variant={campaign.campaign.status === "paused" ? "error" : "default"}
              title={campaign.campaign.status === "paused" ? "Update rollout paused" : `Update rollout · batch ${Math.max(campaign.campaign.current_batch, 0)}`}
              description={campaign.campaign.error || `${campaign.members.filter((member) => member.status === "succeeded").length} of ${campaign.members.length} nodes completed.`}
              action={
                <Button variant="secondary" size="sm" onClick={() => setUpdateDialog({ mode: "all", campaign })}>
                  View rollout
                </Button>
              }
            />
          ) : release?.update_error ? (
            <Banner variant="secondary" title="Managed updates unavailable" description={release.update_error} />
          ) : null}
          <section className="flex flex-col gap-3">
            <div className="flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between">
              <div>
                <h2 className="text-base font-semibold text-kumo-default">Node inventory</h2>
                <p className="text-sm text-kumo-subtle">
                  {total > 0 ? `Showing ${offset + 1}-${Math.min(offset + perPage, total)} of ${total}` : "No nodes"}
                </p>
              </div>
            </div>

            <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
              <form
                className="flex min-w-0 flex-1 gap-2"
                onSubmit={(event) => {
                  event.preventDefault();
                  setSearch(searchInput.trim());
                  setPage(1);
                }}
              >
                <Input
                  placeholder="Search by node name, host, or status"
                  aria-label="Search nodes"
                  value={searchInput}
                  onChange={(event) => setSearchInput(event.target.value)}
                  className="min-w-0 flex-1"
                />
                <Button type="submit" variant="secondary">
                  Search
                </Button>
              </form>
              <DropdownMenu>
                <DropdownMenu.Trigger
                  render={
                    <Button variant="secondary" icon={FunnelIcon}>
                      Filter
                    </Button>
                  }
                />
                <DropdownMenu.Content>
                  <DropdownMenu.Group>
                    <DropdownMenu.Label>Status</DropdownMenu.Label>
                    <DropdownMenu.RadioGroup
                      value={filter}
                      onValueChange={(value) => setFilterValue(value as NodeFilter)}
                    >
                      <DropdownMenu.RadioItem value="all">
                        All
                        <DropdownMenu.RadioItemIndicator />
                      </DropdownMenu.RadioItem>
                      <DropdownMenu.RadioItem value="active">
                        Active
                        <DropdownMenu.RadioItemIndicator />
                      </DropdownMenu.RadioItem>
                      <DropdownMenu.RadioItem value="disabled">
                        Disabled
                        <DropdownMenu.RadioItemIndicator />
                      </DropdownMenu.RadioItem>
                      <DropdownMenu.RadioItem value="degraded">
                        Degraded
                        <DropdownMenu.RadioItemIndicator />
                      </DropdownMenu.RadioItem>
                      <DropdownMenu.RadioItem value="deleted">
                        Deleted
                        <DropdownMenu.RadioItemIndicator />
                      </DropdownMenu.RadioItem>
                    </DropdownMenu.RadioGroup>
                  </DropdownMenu.Group>
                </DropdownMenu.Content>
              </DropdownMenu>
            </div>

            <div className="overflow-hidden rounded-lg border border-kumo-line bg-kumo-base">
              <div className="bf-table-scroll overflow-x-auto overscroll-x-contain">
                <Table className="min-w-[1180px]">
                  <Table.Header variant="compact">
                    <Table.Row>
                      <SortHead label="Node" column="name" sort={sort} direction={direction} setSort={setSort} className="sticky left-0 z-20 bg-kumo-base" />
                      <SortHead label="Status" column="status" sort={sort} direction={direction} setSort={setSort} />
                      <SortHead label="Public host" column="public_host" sort={sort} direction={direction} setSort={setSort} />
                      <Table.Head>Agent</Table.Head>
                      <SortHead label="sing-box" column="sing_box_version" sort={sort} direction={direction} setSort={setSort} />
                      <Table.Head>Config</Table.Head>
                      <SortHead label="Last seen" column="last_seen_at" sort={sort} direction={direction} setSort={setSort} className="hidden xl:table-cell" />
                      <Table.Head>Update</Table.Head>
                      <Table.Head className="text-right">
                        <span className="sr-only">Actions</span>
                      </Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {nodesQuery.error ? (
                      <TableEmpty colSpan={9}>{error}</TableEmpty>
                    ) : nodesQuery.isLoading ? (
                      <TableLoading colSpan={9} />
                    ) : nodes.length > 0 ? (
                      nodes.map((node) => {
                        const health = node.deleted_at
                          ? { label: "Deleted", icon: ProhibitIcon, className: "text-kumo-subtle" }
                          : nodeHealth(node);
                        const StatusIcon = health.icon;
                        const statusClassName = health.label === "Disabled" ? "text-kumo-subtle" : health.className;
                        return (
                          <Table.Row key={node.id}>
                            <Table.Cell className="sticky left-0 z-10 bg-kumo-base">
                              <div className="flex min-w-44 items-center gap-2">
                                <HardDrivesIcon className="size-4 shrink-0 text-kumo-subtle" />
                                <span className="truncate text-base font-medium text-kumo-default" title={node.name}>{node.name}</span>
                              </div>
                            </Table.Cell>
                            <Table.Cell>
                              <span className={`inline-flex items-center gap-1.5 whitespace-nowrap text-sm font-medium ${statusClassName}`}>
                                <StatusIcon className="size-4 shrink-0" />
                                {health.label}
                              </span>
                            </Table.Cell>
                            <Table.Cell>
                              <span className="whitespace-nowrap text-kumo-subtle">
                                {node.public_host || node.api_base_url || "n/a"}
                                {node.hosts && node.hosts.length > 1 ? (
                                  <span className="text-kumo-inactive"> +{node.hosts.length - 1}</span>
                                ) : null}
                              </span>
                            </Table.Cell>
                            <Table.Cell>
                              <VersionTarget current={node.agent_version} target={release?.agent_version ?? node.agent_version ?? "n/a"} />
                            </Table.Cell>
                            <Table.Cell>
                              <VersionTarget current={node.sing_box_version} target={release?.sing_box_version ?? node.sing_box_version ?? "n/a"} />
                            </Table.Cell>
                            <Table.Cell>
                              <span className="whitespace-nowrap text-kumo-subtle">{formatNodeVersion(node)}</span>
                            </Table.Cell>
                            <Table.Cell className="hidden xl:table-cell">
                              <span className="whitespace-nowrap text-kumo-subtle">{formatRelativeTime(nodeTimestamp(node))}</span>
                            </Table.Cell>
                            <Table.Cell>
                              <span className={`inline-flex items-center gap-1.5 whitespace-nowrap text-sm font-medium ${nodeUpdateStatus(node, release).className}`}>
                                <span className="size-1.5 rounded-full bg-current" />
                                {nodeUpdateStatus(node, release).label}
                              </span>
                            </Table.Cell>
                            <Table.Cell className="text-right">
                              <div className="inline-flex items-center gap-1">
                                {node.active_operation ? (
                                  <Button
                                    variant="secondary"
                                    size="sm"
                                    onClick={() => setUpdateDialog({ mode: "node", node })}
                                  >
                                    View
                                  </Button>
                                ) : release?.updates_enabled &&
                                  hasCapability(node, "operations.v1") &&
                                  ((!versionsEqual(node.agent_version, release.agent_version) && hasCapability(node, "update.agent.v1")) ||
                                    (!versionsEqual(node.sing_box_version, release.sing_box_version) && hasCapability(node, "update.sing_box.v1"))) &&
                                  !node.deleted_at && node.has_active_token !== false ? (
                                  <Button size="sm" onClick={() => setUpdateDialog({ mode: "node", node })}>
                                    Update
                                  </Button>
                                ) : null}
                                <DropdownMenu>
                                <DropdownMenu.Trigger
                                  render={
                                    <Button variant="ghost" size="sm" shape="square" aria-label={`Actions for ${node.name}`}>
                                      <DotsThreeIcon className="size-4" />
                                    </Button>
                                  }
                                />
                                <DropdownMenu.Content>
                                  {node.deleted_at ? (
                                    <DropdownMenu.Item icon={ArrowsClockwiseIcon} onClick={() => restore.mutate(node)}>
                                      Restore
                                    </DropdownMenu.Item>
                                  ) : (
                                    <>
                                  {node.active_operation ? (
                                    <DropdownMenu.Item icon={DownloadSimpleIcon} onClick={() => setUpdateDialog({ mode: "node", node })}>
                                      View update
                                    </DropdownMenu.Item>
                                  ) : release?.updates_enabled && hasCapability(node, "operations.v1") ? (
                                    <>
                                      {!versionsEqual(node.agent_version, release.agent_version) && hasCapability(node, "update.agent.v1") ? (
                                        <DropdownMenu.Item icon={DownloadSimpleIcon} onClick={() => setUpdateDialog({ mode: "node", node, components: ["agent"] })}>
                                          Update agent
                                        </DropdownMenu.Item>
                                      ) : null}
                                      {!versionsEqual(node.sing_box_version, release.sing_box_version) && hasCapability(node, "update.sing_box.v1") ? (
                                        <DropdownMenu.Item icon={DownloadSimpleIcon} onClick={() => setUpdateDialog({ mode: "node", node, components: ["sing_box"] })}>
                                          Update sing-box
                                        </DropdownMenu.Item>
                                      ) : null}
                                      <DropdownMenu.Separator />
                                    </>
                                  ) : null}
                                  <DropdownMenu.Item icon={PencilSimpleIcon} onClick={() => setDialog({ mode: "edit", node })}>
                                    Edit
                                  </DropdownMenu.Item>
                                  {node.status === "pending" ? (
                                    // Pending: the one-time bootstrap may have been lost before the
                                    // agent checked in. Let the operator re-show the install command.
                                    <DropdownMenu.Item icon={DownloadSimpleIcon} onClick={() => setDialog({ mode: "reenroll", node })}>
                                      Show install command
                                    </DropdownMenu.Item>
                                  ) : null}
                                  {node.status === "disabled" && node.has_active_token === false ? (
                                    // Decommissioned: tokens were revoked, so Enable would yield an
                                    // active node whose agent can never authenticate. Re-enroll issues
                                    // a fresh token + bootstrap to bring it back online.
                                    <DropdownMenu.Item icon={ArrowsClockwiseIcon} onClick={() => setDialog({ mode: "reenroll", node })}>
                                      Re-enroll
                                    </DropdownMenu.Item>
                                  ) : (
                                    <>
                                      <DropdownMenu.Item
                                        icon={node.status === "disabled" ? CheckCircleIcon : ProhibitIcon}
                                        onClick={() => toggleStatus.mutate(node)}
                                      >
                                        {node.status === "disabled" ? "Enable" : "Disable"}
                                      </DropdownMenu.Item>
                                      <DropdownMenu.Separator />
                                      <DropdownMenu.Item variant="danger" icon={TrashIcon} onClick={() => setDialog({ mode: "delete", node })}>
                                        Delete
                                      </DropdownMenu.Item>
                                    </>
                                  )}
                                    </>
                                  )}
                                </DropdownMenu.Content>
                                </DropdownMenu>
                              </div>
                            </Table.Cell>
                          </Table.Row>
                        );
                      })
                    ) : (
                      <TableEmpty colSpan={9}>No nodes match this filter.</TableEmpty>
                    )}
                  </Table.Body>
                </Table>
              </div>
            </div>

            <AdminPagination page={page} setPage={setPage} perPage={perPage} setPerPage={setPageSize} total={total} />
          </section>
        </div>
      </main>

      {dialog?.mode === "enroll" ? (
        <EnrollNodeDialog request={request} onClose={() => setDialog(null)} />
      ) : null}
      {dialog?.mode === "edit" ? (
        <EditNodeDialog request={request} node={dialog.node} onClose={() => setDialog(null)} />
      ) : null}
      {dialog?.mode === "delete" ? (
        <DeleteNodeDialog request={request} node={dialog.node} onClose={() => setDialog(null)} />
      ) : null}
      {dialog?.mode === "reenroll" ? (
        <ReenrollNodeDialog request={request} node={dialog.node} onClose={() => setDialog(null)} />
      ) : null}
      {updateDialog?.mode === "node" && release ? (
        <NodeUpdateDialog
          request={request}
          node={updateDialog.node}
          release={release}
          initialComponents={updateDialog.components}
          initialOperation={updateDialog.node.active_operation}
          onClose={() => setUpdateDialog(null)}
        />
      ) : null}
      {updateDialog?.mode === "all" && release ? (
        <UpdateAllDialog
          request={request}
          release={release}
          initialCampaign={updateDialog.campaign}
          onClose={() => setUpdateDialog(null)}
        />
      ) : null}
    </div>
  );
}
