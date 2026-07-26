import { useEffect, useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useQuery } from "@tanstack/react-query";
import { Badge, Banner, Button, Dialog, DropdownMenu, Input, Select, Switch, Table } from "@cloudflare/kumo";
import { CheckCircleIcon, PencilSimpleIcon, PlusIcon, ProhibitIcon, TrashIcon } from "@phosphor-icons/react";

import { useAdminApi } from "@/admin/api";
import { adminKeys, refreshIntervals } from "@/admin/query";
import { useAdminMutation } from "@/admin/use-admin-mutation";
import { AppPageHeader } from "@/components/app-page-header";
import { RowActionsMenu } from "@/components/row-actions-menu";
import { StatusBadge } from "@/components/status-badge";
import { TableCard, TableEmpty, TableError, TableLoading } from "@/components/admin-table";
import type { AdminNode, AdminPath, AdminProxiesResponse } from "../types";
import { SoftDeleteDialog } from "./soft-delete-dialog";

type EditorState = { path?: AdminPath } | null;

const VISIBILITY_LABELS: Record<AdminPath["visibility"], string> = {
  selectable: "Selectable",
  dependency: "Dependency only"
};

function pathLabel(path: AdminPath): string {
  return path.display_name || `${path.proxy_name} · ${path.name}`;
}

export function PathsPage() {
  const { request } = useAdminApi();
  const [editor, setEditor] = useState<EditorState>(null);
  const [deleteTarget, setDeleteTarget] = useState<AdminPath | null>(null);
  const pathsQuery = useQuery({
    queryKey: adminKeys.paths,
    queryFn: () => request<AdminPath[]>("/api/admin/paths"),
    refetchInterval: refreshIntervals.slow
  });
  const toggleEnabled = useAdminMutation<AdminPath>(request, (req, path) =>
    req(`/api/admin/paths/${encodeURIComponent(path.id)}`, {
      method: "PATCH",
      body: JSON.stringify({ enabled: !path.enabled })
    })
  );
  const paths = pathsQuery.data ?? [];

  return (
    <div className="flex min-h-full flex-col bg-kumo-canvas">
      <AppPageHeader
        title="Paths"
        description="Publish a Proxy through a specific Host, optionally using another Path as its Mihomo dialer."
        actions={
          <Button variant="primary" icon={PlusIcon} onClick={() => setEditor({})}>
            Create
          </Button>
        }
      />
      <main className="w-full grow bg-kumo-canvas">
        <div className="mx-auto flex w-full max-w-[1400px] flex-col gap-4 px-6 pb-8 md:px-8 lg:px-10">
          <section className="flex flex-col gap-3">
            <div>
              <h2 className="text-base font-semibold text-kumo-default">Published paths</h2>
              <p className="text-sm text-kumo-subtle">
                {paths.length === 0 ? "No paths yet" : `${paths.length} ${paths.length === 1 ? "path" : "paths"}`}
              </p>
            </div>
            <TableCard>
              <Table className="min-w-[900px]">
                <Table.Header variant="compact">
                  <Table.Row>
                    <Table.Head>Published name</Table.Head>
                    <Table.Head>Endpoint</Table.Head>
                    <Table.Head>Dialer Path</Table.Head>
                    <Table.Head>Visibility</Table.Head>
                    <Table.Head>Status</Table.Head>
                    <Table.Head className="text-right">
                      <span className="sr-only">Actions</span>
                    </Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {pathsQuery.error ? (
                    <TableError colSpan={6}>
                      {pathsQuery.error instanceof Error ? pathsQuery.error.message : "Request failed."}
                    </TableError>
                  ) : pathsQuery.isLoading ? (
                    <TableLoading colSpan={6} />
                  ) : paths.length === 0 ? (
                    <TableEmpty colSpan={6} description="Create a Path to publish a Proxy to users.">
                      No Paths yet
                    </TableEmpty>
                  ) : (
                    paths.map((path) => {
                      const dialer = paths.find((candidate) => candidate.id === path.dialer_path_id);
                      return (
                        <Table.Row key={path.id}>
                          <Table.Cell>
                            <div className="flex min-w-48 items-center gap-2">
                              <span className="truncate font-medium text-kumo-default" title={pathLabel(path)}>
                                {pathLabel(path)}
                              </span>
                              {path.managed ? <Badge variant="secondary">Managed</Badge> : null}
                            </div>
                            <div className="text-xs text-kumo-subtle">{path.name}</div>
                          </Table.Cell>
                          <Table.Cell>
                            <span className="whitespace-nowrap text-kumo-subtle">
                              {path.proxy_name} @ {path.host_tag || path.host}
                            </span>
                          </Table.Cell>
                          <Table.Cell>
                            <span className="text-kumo-subtle">{dialer ? pathLabel(dialer) : "Direct"}</span>
                          </Table.Cell>
                          <Table.Cell>
                            <span className="whitespace-nowrap text-kumo-subtle">{VISIBILITY_LABELS[path.visibility]}</span>
                          </Table.Cell>
                          <Table.Cell>
                            <StatusBadge tone={path.enabled ? "success" : "neutral"}>
                              {path.enabled ? "Enabled" : "Disabled"}
                            </StatusBadge>
                          </Table.Cell>
                          <Table.Cell className="text-right">
                            {!path.managed ? (
                              <RowActionsMenu label={`Actions for ${path.name}`}>
                                <DropdownMenu.Item icon={PencilSimpleIcon} onClick={() => setEditor({ path })}>
                                  Edit
                                </DropdownMenu.Item>
                                <DropdownMenu.Item
                                  icon={path.enabled ? ProhibitIcon : CheckCircleIcon}
                                  disabled={toggleEnabled.isPending}
                                  onClick={() => toggleEnabled.mutate(path)}
                                >
                                  {path.enabled ? "Disable" : "Enable"}
                                </DropdownMenu.Item>
                                <DropdownMenu.Separator />
                                <DropdownMenu.Item variant="danger" icon={TrashIcon} onClick={() => setDeleteTarget(path)}>
                                  Delete
                                </DropdownMenu.Item>
                              </RowActionsMenu>
                            ) : null}
                          </Table.Cell>
                        </Table.Row>
                      );
                    })
                  )}
                </Table.Body>
              </Table>
            </TableCard>
          </section>
        </div>
      </main>
      {editor ? <PathEditor request={request} state={editor} paths={paths} onClose={() => setEditor(null)} /> : null}
      {deleteTarget ? (
        <SoftDeleteDialog
          request={request}
          title="Delete path"
          description={
            <>
              Delete <span className="font-medium text-kumo-default">{pathLabel(deleteTarget)}</span>? This permanently
              removes the Path and any client selections that use it.
            </>
          }
          endpoint={`/api/admin/paths/${encodeURIComponent(deleteTarget.id)}`}
          onClose={() => setDeleteTarget(null)}
        />
      ) : null}
    </div>
  );
}

const pathFormSchema = z.object({
  name: z.string().trim().min(1, "Route name is required"),
  display_name: z.string(),
  proxy_id: z.string().min(1, "Select a proxy"),
  host_id: z.string().min(1, "Select a host"),
  dialer_path_id: z.string(),
  visibility: z.enum(["selectable", "dependency"]),
  enabled: z.boolean()
});

type PathFormValues = z.infer<typeof pathFormSchema>;

function editorDefaults(existing: AdminPath | undefined): PathFormValues {
  return {
    name: existing?.name ?? "",
    display_name: existing?.display_name ?? "",
    proxy_id: existing?.proxy_id ?? "",
    host_id: existing?.host_id ?? "",
    dialer_path_id: existing?.dialer_path_id ?? "",
    visibility: existing?.visibility ?? "selectable",
    enabled: existing?.enabled ?? true
  };
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
  const form = useForm<PathFormValues>({
    resolver: zodResolver(pathFormSchema),
    defaultValues: editorDefaults(existing)
  });
  useEffect(() => {
    form.reset(editorDefaults(existing));
  }, [form, existing]);

  const proxiesQuery = useQuery({
    queryKey: adminKeys.proxies,
    queryFn: () => request<AdminProxiesResponse>("/api/admin/proxies?limit=500")
  });
  const nodesQuery = useQuery({
    queryKey: adminKeys.nodes,
    queryFn: () => request<AdminNode[]>("/api/admin/nodes")
  });
  const loading = proxiesQuery.isLoading || nodesQuery.isLoading;
  const loadError = proxiesQuery.error ?? nodesQuery.error;

  const proxyID = form.watch("proxy_id");
  const hostID = form.watch("host_id");

  // Offer enabled proxies; when editing a Path whose Proxy is meanwhile
  // disabled, keep the current value selectable instead of silently switching.
  const allProxies = useMemo(() => proxiesQuery.data?.proxies ?? [], [proxiesQuery.data]);
  const proxyItems = useMemo(() => {
    const items = allProxies
      .filter((proxy) => proxy.enabled)
      .map((proxy) => ({ value: proxy.id, label: `${proxy.node_name} / ${proxy.name}` }));
    if (existing && !items.some((item) => item.value === existing.proxy_id)) {
      items.unshift({
        value: existing.proxy_id,
        label: `${existing.node_name} / ${existing.proxy_name} (disabled)`
      });
    }
    return items;
  }, [allProxies, existing]);

  const selectedProxy =
    allProxies.find((proxy) => proxy.id === proxyID) ??
    (existing && existing.proxy_id === proxyID
      ? { id: existing.proxy_id, node_name: existing.node_name }
      : undefined);
  const hostItems = useMemo(() => {
    const hosts =
      (nodesQuery.data ?? [])
        .find((node) => node.name === selectedProxy?.node_name)
        ?.hosts?.filter((host) => host.selected) ?? [];
    const items = hosts.map((host) => ({
      value: host.id,
      label: host.tag ? `${host.tag} · ${host.host}` : host.host
    }));
    if (existing && existing.proxy_id === proxyID && !items.some((item) => item.value === existing.host_id)) {
      items.unshift({
        value: existing.host_id,
        label: `${existing.host_tag || existing.host} (unavailable)`
      });
    }
    return items;
  }, [nodesQuery.data, selectedProxy?.node_name, existing, proxyID]);

  // Convenience: a proxy's node usually has exactly one selected host.
  useEffect(() => {
    if (proxyID && !hostID && hostItems.length === 1) {
      form.setValue("host_id", hostItems[0].value, { shouldValidate: true });
    }
  }, [form, proxyID, hostID, hostItems]);

  const dialerItems = useMemo(
    () => [
      { value: "direct", label: "Direct" },
      ...paths
        .filter((path) => path.id !== existing?.id && path.enabled)
        .map((path) => ({ value: path.id, label: pathLabel(path) }))
    ],
    [paths, existing?.id]
  );

  const save = useAdminMutation<PathFormValues, AdminPath>(
    request,
    (req, values) =>
      req(existing ? `/api/admin/paths/${encodeURIComponent(existing.id)}` : "/api/admin/paths", {
        method: existing ? "PATCH" : "POST",
        body: JSON.stringify({
          name: values.name.trim(),
          display_name: values.display_name.trim(),
          proxy_id: values.proxy_id,
          host_id: values.host_id,
          dialer_path_id: values.dialer_path_id,
          visibility: values.visibility,
          enabled: values.enabled,
          sort_order: existing?.sort_order ?? 0
        })
      }),
    { onSuccess: onClose, toastError: false }
  );

  const errors = form.formState.errors;

  return (
    <Dialog.Root open onOpenChange={(open) => (open || save.isPending ? undefined : onClose())}>
      <Dialog size="base" className="max-h-[calc(100dvh-2rem)] overflow-y-auto p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">
          {existing ? `Edit ${existing.name}` : "Create Path"}
        </Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          An Endpoint is created automatically from the selected Proxy and Host.
        </Dialog.Description>

        {loadError ? (
          <Banner
            variant="error"
            title={loadError instanceof Error ? loadError.message : "Failed to load proxies and nodes."}
            className="mb-4"
          />
        ) : null}
        {save.isError ? <Banner variant="error" title={save.error.message} className="mb-4" /> : null}

        <form
          className="flex flex-col gap-4"
          onSubmit={form.handleSubmit((values) => save.mutate(values))}
        >
          <Input label="Route name" error={errors.name?.message} {...form.register("name")} />
          <Input
            label="Published name (optional)"
            placeholder="Defaults to Proxy · Route"
            {...form.register("display_name")}
          />
          <Select
            label="Proxy"
            value={proxyID || null}
            placeholder="Select a proxy"
            loading={loading}
            disabled={loading}
            items={proxyItems}
            error={errors.proxy_id?.message}
            onValueChange={(value) => {
              const next = (value as string) ?? "";
              form.setValue("proxy_id", next, { shouldValidate: true });
              form.setValue("host_id", "", { shouldValidate: false });
            }}
          />
          <Select
            label="Host"
            value={hostID || null}
            placeholder="Select a host"
            loading={loading}
            disabled={loading || !proxyID}
            items={hostItems}
            error={errors.host_id?.message}
            onValueChange={(value) => form.setValue("host_id", (value as string) ?? "", { shouldValidate: true })}
          />
          <Select
            label="Dialer Path"
            value={form.watch("dialer_path_id") || "direct"}
            items={dialerItems}
            onValueChange={(value) =>
              form.setValue("dialer_path_id", value === "direct" ? "" : ((value as string) ?? ""))
            }
          />
          <Select
            label="Visibility"
            value={form.watch("visibility")}
            items={[
              { value: "selectable", label: VISIBILITY_LABELS.selectable },
              { value: "dependency", label: VISIBILITY_LABELS.dependency }
            ]}
            onValueChange={(value) => form.setValue("visibility", value as PathFormValues["visibility"])}
          />
          <Switch
            label="Enabled"
            controlFirst={false}
            checked={form.watch("enabled")}
            onCheckedChange={(value) => form.setValue("enabled", Boolean(value))}
          />
          <div className="mt-2 flex justify-end gap-2">
            <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={save.isPending}>
              {existing ? "Save changes" : "Create Path"}
            </Button>
          </div>
        </form>
      </Dialog>
    </Dialog.Root>
  );
}
