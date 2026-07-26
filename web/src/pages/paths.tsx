import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button, Dialog, Input, Select, Switch, Table } from "@cloudflare/kumo";
import { PencilSimpleIcon, PlusIcon, TrashIcon } from "@phosphor-icons/react";

import { useAdminApi } from "@/admin/api";
import { adminKeys } from "@/admin/query";
import { useAdminMutation } from "@/admin/use-admin-mutation";
import { TableEmpty, TableLoading } from "@/components/admin-table";
import type { AdminNode, AdminPath, AdminProxiesResponse, AdminProxy } from "../types";
import { PageHeader, PageTopBar } from "./operations-common";

type EditorState = { path?: AdminPath } | null;

export function PathsPage() {
  const { request } = useAdminApi();
  const [editor, setEditor] = useState<EditorState>(null);
  const pathsQuery = useQuery({
    queryKey: adminKeys.paths,
    queryFn: () => request<AdminPath[]>("/api/admin/paths")
  });
  const remove = useAdminMutation<AdminPath>(request, (req, path) =>
    req(`/api/admin/paths/${encodeURIComponent(path.id)}`, {
      method: "DELETE"
    })
  );
  const paths = pathsQuery.data ?? [];

  return (
    <div className="flex min-h-full flex-col bg-kumo-canvas">
      <PageTopBar current="Paths" />
      <main className="w-full grow bg-kumo-canvas">
        <PageHeader
          title="Paths"
          description="Publish a Proxy through a specific Host, optionally using another Path as its Mihomo dialer."
          actions={
            <Button icon={PlusIcon} onClick={() => setEditor({})}>
              Create
            </Button>
          }
        />
        <div className="mx-auto w-full max-w-[1400px] px-6 pb-8 md:px-8 lg:px-10">
          <div className="overflow-x-auto rounded-md border border-kumo-line bg-kumo-base">
            <Table>
              <Table.Header variant="compact">
                <Table.Row>
                  <Table.Head>Published name</Table.Head>
                  <Table.Head>Endpoint</Table.Head>
                  <Table.Head>Dialer Path</Table.Head>
                  <Table.Head>Visibility</Table.Head>
                  <Table.Head>Status</Table.Head>
                  <Table.Head>
                    <span className="sr-only">Actions</span>
                  </Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {pathsQuery.isLoading ? (
                  <TableLoading colSpan={6} />
                ) : pathsQuery.error ? (
                  <TableEmpty colSpan={6}>
                    {pathsQuery.error instanceof Error ? pathsQuery.error.message : "Request failed"}
                  </TableEmpty>
                ) : paths.length === 0 ? (
                  <TableEmpty colSpan={6}>No Paths yet.</TableEmpty>
                ) : (
                  paths.map((path) => {
                    const dialer = paths.find((candidate) => candidate.id === path.dialer_path_id);
                    return (
                      <Table.Row key={path.id}>
                        <Table.Cell>
                          <div className="font-medium text-kumo-default">
                            {path.display_name || `${path.proxy_name} · ${path.name}`}
                          </div>
                          <div className="text-xs text-kumo-subtle">
                            {path.name}
                            {path.managed ? " · managed direct" : ""}
                          </div>
                        </Table.Cell>
                        <Table.Cell>
                          <span className="whitespace-nowrap text-kumo-subtle">
                            {path.proxy_name} @ {path.host_tag || path.host}
                          </span>
                        </Table.Cell>
                        <Table.Cell>
                          <span className="text-kumo-subtle">
                            {dialer ? dialer.display_name || `${dialer.proxy_name} · ${dialer.name}` : "Direct"}
                          </span>
                        </Table.Cell>
                        <Table.Cell>
                          <span className="text-kumo-subtle">{path.visibility}</span>
                        </Table.Cell>
                        <Table.Cell>
                          <span className={path.enabled ? "text-kumo-success" : "text-kumo-subtle"}>
                            {path.enabled ? "Enabled" : "Disabled"}
                          </span>
                        </Table.Cell>
                        <Table.Cell>
                          {!path.managed ? (
                            <div className="flex justify-end gap-1">
                              <Button
                                variant="ghost"
                                size="sm"
                                shape="square"
                                aria-label={`Edit ${path.name}`}
                                onClick={() => setEditor({ path })}
                              >
                                <PencilSimpleIcon />
                              </Button>
                              <Button
                                variant="ghost"
                                size="sm"
                                shape="square"
                                aria-label={`Delete ${path.name}`}
                                loading={remove.isPending}
                                onClick={() => remove.mutate(path)}
                              >
                                <TrashIcon className="text-kumo-danger" />
                              </Button>
                            </div>
                          ) : null}
                        </Table.Cell>
                      </Table.Row>
                    );
                  })
                )}
              </Table.Body>
            </Table>
          </div>
        </div>
      </main>
      {editor ? <PathEditor request={request} state={editor} paths={paths} onClose={() => setEditor(null)} /> : null}
    </div>
  );
}

function PathEditor({
  request,
  state,
  paths,
  onClose
}: {
  request: ReturnType<typeof useAdminApi>["request"];
  state: Exclude<EditorState, null>;
  paths: AdminPath[];
  onClose: () => void;
}) {
  const existing = state.path;
  const proxiesQuery = useQuery({
    queryKey: adminKeys.proxies,
    queryFn: () => request<AdminProxiesResponse>("/api/admin/proxies?limit=500")
  });
  const nodesQuery = useQuery({
    queryKey: adminKeys.nodes,
    queryFn: () => request<AdminNode[]>("/api/admin/nodes")
  });
  const proxies = useMemo(() => (proxiesQuery.data?.proxies ?? []).filter((p) => p.enabled), [proxiesQuery.data]);
  const [name, setName] = useState(existing?.name ?? "");
  const [displayName, setDisplayName] = useState(existing?.display_name ?? "");
  const [selectedProxyID, setSelectedProxyID] = useState(existing?.proxy_id ?? "");
  const [selectedHostID, setSelectedHostID] = useState(existing?.host_id ?? "");
  const [dialerPathID, setDialerPathID] = useState(existing?.dialer_path_id ?? "");
  const [visibility, setVisibility] = useState<AdminPath["visibility"]>(existing?.visibility ?? "selectable");
  const [enabled, setEnabled] = useState(existing?.enabled ?? true);
  const proxyID = selectedProxyID || proxies[0]?.id || "";
  const selectedProxy =
    proxies.find((proxy) => proxy.id === proxyID) ??
    (existing ? ({ id: existing.proxy_id, node_name: existing.node_name } as AdminProxy) : undefined);
  const hosts = useMemo(
    () =>
      (nodesQuery.data ?? [])
      .find((node) => node.name === selectedProxy?.node_name)
      ?.hosts?.filter((host) => host.selected) ?? [],
    [nodesQuery.data, selectedProxy?.node_name]
  );
  const hostID = hosts.some((host) => host.id === selectedHostID) ? selectedHostID : hosts[0]?.id || "";

  const save = useAdminMutation<void, AdminPath>(
    request,
    (req) =>
      req(existing ? `/api/admin/paths/${encodeURIComponent(existing.id)}` : "/api/admin/paths", {
        method: existing ? "PATCH" : "POST",
        body: JSON.stringify({
          name: name.trim(),
          display_name: displayName.trim(),
          proxy_id: proxyID,
          host_id: hostID,
          dialer_path_id: dialerPathID,
          visibility,
          enabled,
          sort_order: existing?.sort_order ?? 0
        })
      }),
    { onSuccess: onClose }
  );

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog size="base" className="max-h-[calc(100dvh-2rem)] overflow-y-auto p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">
          {existing ? `Edit ${existing.name}` : "Create Path"}
        </Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          An Endpoint is created automatically from the selected Proxy and Host.
        </Dialog.Description>
        <form
          className="flex flex-col gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            save.mutate();
          }}
        >
          <Input label="Route name" value={name} onChange={(event) => setName(event.target.value)} required />
          <Input
            label="Published name (optional)"
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
            placeholder="Defaults to Proxy · Route"
          />
          <Select
            label="Proxy"
            value={proxyID || null}
            items={proxies.map((proxy) => ({
              value: proxy.id,
              label: `${proxy.node_name} / ${proxy.name}`
            }))}
            onValueChange={(value) => {
              setSelectedProxyID((value as string) ?? "");
              setSelectedHostID("");
            }}
          />
          <Select
            label="Host"
            value={hostID || null}
            items={hosts.map((host) => ({
              value: host.id,
              label: host.tag ? `${host.tag} · ${host.host}` : host.host
            }))}
            onValueChange={(value) => setSelectedHostID((value as string) ?? "")}
          />
          <Select
            label="Dialer Path"
            value={dialerPathID || "direct"}
            items={[
              { value: "direct", label: "Direct" },
              ...paths
                .filter((path) => path.id !== existing?.id && path.enabled)
                .map((path) => ({
                  value: path.id,
                  label: path.display_name || `${path.proxy_name} · ${path.name}`
                }))
            ]}
            onValueChange={(value) => setDialerPathID(value === "direct" ? "" : ((value as string) ?? ""))}
          />
          <Select
            label="Visibility"
            value={visibility}
            items={[
              { value: "selectable", label: "Selectable" },
              { value: "dependency", label: "Dependency only" }
            ]}
            onValueChange={(value) => setVisibility(value as AdminPath["visibility"])}
          />
          <Switch label="Enabled" controlFirst={false} checked={enabled} onCheckedChange={setEnabled} />
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={save.isPending} disabled={!name.trim() || !proxyID || !hostID}>
              Save
            </Button>
          </div>
        </form>
      </Dialog>
    </Dialog.Root>
  );
}
