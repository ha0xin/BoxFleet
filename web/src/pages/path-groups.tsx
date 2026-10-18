import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { getCoreRowModel, getPaginationRowModel, useReactTable, type ColumnDef } from "@tanstack/react-table";
import { Banner, Button, Checkbox, Dialog, DropdownMenu, Input, Switch, Table } from "@cloudflare/kumo";
import { ArchiveIcon, PencilSimpleIcon } from "@phosphor-icons/react";
import type { AdminRequest } from "@/admin/api";
import { adminKeys, refreshIntervals } from "@/admin/query";
import { useAdminMutation } from "@/admin/use-admin-mutation";
import { AdminPagination, TableCard, TableColgroup, TableEmpty, TableError, TableLoading, tableMinWidth, type TableColumnWidth } from "@/components/admin-table";
import { RowActionsMenu } from "@/components/row-actions-menu";
import { StatusBadge } from "@/components/status-badge";
import type { AdminPath } from "@/types";

export type PathGroup = {
  selected_user_count?: number; id: string; name: string; description: string; enabled: boolean; revision: number; path_ids: string[]; user_ids: string[];
};
type GroupChange = {
  operation: "save" | "delete" | "grant" | "revoke";
  group_id?: string; user_id?: string; name?: string; description?: string; enabled?: boolean; revision?: number; path_ids?: string[];
};
type GroupImpact = {
  preview_token: string; group_id: string; node_ids: string[];
  users: Array<{ user_id: string; user_name: string; added: string[]; removed: string[]; retained: string[] }>;
};

export function usePathGroups(request: AdminRequest) {
  return useQuery({ queryKey: adminKeys.pathGroups, queryFn: ({ signal }) => request<PathGroup[]>("/api/admin/path-groups", { signal }), refetchInterval: refreshIntervals.slow });
}

export function usePathGroupChange(request: AdminRequest, onSuccess?: () => void) {
  return useAdminMutation<GroupChange, GroupImpact>(request, async (req, change) => {
    // The API token still guards against concurrent changes; no extra user step.
    const preview = await req<GroupImpact>("/api/admin/path-groups/preview", {
      method: "POST", body: JSON.stringify(change)
    });
    return req<GroupImpact>("/api/admin/path-groups/changes", {
      method: "POST", body: JSON.stringify({ ...change, preview_token: preview.preview_token })
    });
  }, { onSuccess, toastError: false });
}

const groupSchema = z.object({
  name: z.string().trim().min(1, "Group name is required").max(100), description: z.string().max(2000), enabled: z.boolean(), path_ids: z.array(z.string()).max(200)
});
type GroupValues = z.infer<typeof groupSchema>;

export function PathGroupEditor({ request, group, paths, onClose }: { request: AdminRequest; group?: PathGroup; paths: AdminPath[]; onClose: () => void }) {
  const save = usePathGroupChange(request, onClose);
  const form = useForm<GroupValues>({ resolver: zodResolver(groupSchema), defaultValues: {
    name: group?.name ?? "", description: group?.description ?? "", enabled: group?.enabled ?? true, path_ids: group?.path_ids ?? []
  } });
  const selected = form.watch("path_ids");
  const selectable = paths.filter((p) => p.visibility === "selectable" || selected.includes(p.id));
  return <>
    <Dialog.Root open onOpenChange={(open) => { if (!open && !save.isPending) onClose(); }}>
      <Dialog size="lg" className="max-h-[calc(100dvh-2rem)] overflow-y-auto p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">{group ? "Edit Path group" : "Create Path group"}</Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">Users granted this group automatically follow its Path membership.</Dialog.Description>
        {save.error ? <Banner variant="error" title={save.error.message} className="mb-4" /> : null}
        <form className="flex flex-col gap-4" onSubmit={form.handleSubmit((values) => save.mutate({ operation: "save", group_id: group?.id, revision: group?.revision, ...values }))}>
          <Input label="Group name" {...form.register("name")} error={form.formState.errors.name?.message} />
          <Input label="Description" {...form.register("description")} error={form.formState.errors.description?.message} />
          <Switch label="Enabled" checked={form.watch("enabled")} onCheckedChange={(checked) => form.setValue("enabled", checked)} />
          <Checkbox.Group legend="Member Paths" error={form.formState.errors.path_ids?.message}>
            <div className="flex max-h-64 flex-col gap-3 overflow-y-auto rounded-md border border-kumo-line p-3">
              {selectable.map((p) => <div key={p.id} className="shrink-0"><Checkbox label={p.display_name || `${p.proxy_name} · ${p.name}`} disabled={!p.enabled} checked={selected.includes(p.id)} onCheckedChange={(checked) => form.setValue("path_ids", checked ? [...selected, p.id] : selected.filter((id) => id !== p.id), { shouldValidate: true })} /></div>)}
              {!selectable.length ? <p className="text-sm text-kumo-subtle">Create a selectable Path first, or save an empty group.</p> : null}
            </div>
          </Checkbox.Group>
          <div className="flex justify-end gap-2"><Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>Cancel</Button><Button type="submit" loading={save.isPending}>{group ? "Save changes" : "Create group"}</Button></div>
        </form>
      </Dialog>
    </Dialog.Root>
  </>;
}

const groupColumns: TableColumnWidth[] = [{ min: 180 }, 88, 88, 116, 52];

export function PathGroupsPanel({ request, paths }: { request: AdminRequest; paths: AdminPath[] }) {
  const groups = usePathGroups(request);
  const [editor, setEditor] = useState<{ group?: PathGroup } | null>(null);
  const archive = usePathGroupChange(request);
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(10);
  const columns = useMemo<ColumnDef<PathGroup>[]>(() => [{ accessorKey: "name" }], []);
  const data = groups.data ?? [];
  const currentPage = Math.min(page, Math.max(1, Math.ceil(data.length / perPage)));
  const table = useReactTable({ data, columns, getCoreRowModel: getCoreRowModel(), getPaginationRowModel: getPaginationRowModel(), state: { pagination: { pageIndex: currentPage - 1, pageSize: perPage } } });
  return <section className="flex flex-col gap-3" aria-label="Path groups">
    {archive.error ? <Banner variant="error" title={archive.error.message} /> : null}
    <TableCard tableId="path-groups" widths={groupColumns}><Table layout="fixed" style={{ minWidth: tableMinWidth(groupColumns) }}>
      <TableColgroup widths={groupColumns} />
      <Table.Header variant="compact"><Table.Row><Table.Head>Group</Table.Head><Table.Head>Members</Table.Head><Table.Head>Users</Table.Head><Table.Head>Status</Table.Head><Table.Head className="text-right"><span className="sr-only">Actions</span></Table.Head></Table.Row></Table.Header>
      <Table.Body>{groups.error ? <TableError colSpan={5}>{groups.error.message}</TableError> : groups.isLoading ? <TableLoading colSpan={5} /> : !data.length ? <TableEmpty colSpan={5}>No Path groups yet</TableEmpty> : table.getRowModel().rows.map(({ original: g }) => <Table.Row key={g.id}>
        <Table.Cell><Button variant="ghost" className="bf-resource-link" title={g.name} onClick={() => setEditor({ group: g })}>{g.name}</Button></Table.Cell>
        <Table.Cell>{g.path_ids.length}</Table.Cell><Table.Cell>{g.selected_user_count ?? 0}</Table.Cell>
        <Table.Cell><StatusBadge tone={g.enabled ? "success" : "neutral"}>{g.enabled ? "Enabled" : "Disabled"}</StatusBadge></Table.Cell>
        <Table.Cell className="text-right">
          <RowActionsMenu label={`Actions for ${g.name}`}>
            <DropdownMenu.Item icon={PencilSimpleIcon} onClick={() => setEditor({ group: g })}>Edit</DropdownMenu.Item>
            <DropdownMenu.Separator />
            <DropdownMenu.Item variant="danger" icon={ArchiveIcon} disabled={archive.isPending} onClick={() => archive.mutate({ operation: "delete", group_id: g.id, revision: g.revision })}>Archive group</DropdownMenu.Item>
          </RowActionsMenu>
        </Table.Cell>
      </Table.Row>)}</Table.Body>
    </Table></TableCard>
    <AdminPagination page={currentPage} setPage={setPage} perPage={perPage} setPerPage={setPerPage} total={data.length} />
    {editor ? <PathGroupEditor request={request} group={editor.group} paths={paths} onClose={() => setEditor(null)} /> : null}
  </section>;
}
