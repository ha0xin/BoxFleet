import { useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { CaretDownIcon, CopyIcon } from "@phosphor-icons/react";
import { Banner, Button, Checkbox, Collapsible, Dialog, Input, Loader, Select } from "@cloudflare/kumo";

import type {
  AdminPath,
  AdminPathAccess,
  AdminUser,
  UserConnectionInfo
} from "../types";
import type { AdminRequest } from "@/admin/api";
import { adminKeys } from "@/admin/query";
import { useAdminMutation } from "@/admin/use-admin-mutation";
import { useQuery } from "@tanstack/react-query";
import { copyText as writeClipboard } from "@/utils";
import { usePathGroups } from "./path-groups";

export type UserDialogState =
  | { mode: "create" }
  | { mode: "edit"; user: AdminUser }
  | { mode: "access"; user: AdminUser }
  | { mode: "connection"; user: AdminUser }
  | null;

const GIB = 1024 ** 3;

function bytesToGib(bytes: number): number {
  if (!bytes) return 0;
  return Math.round((bytes / GIB) * 100) / 100;
}

function gibToBytes(gib: number): number {
  return Math.round(gib * GIB);
}

// datetime-local <input> works in the browser's local timezone; the API stores
// RFC3339 UTC. Convert at the boundary so the value round-trips.
function toLocalInput(iso: string): string {
  if (!iso) return "";
  const date = new Date(iso);
  if (!Number.isFinite(date.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function fromLocalInput(value: string): string {
  if (!value.trim()) return "";
  const date = new Date(value);
  return Number.isFinite(date.getTime()) ? date.toISOString() : "";
}

const userFormSchema = z.object({
  name: z.string().min(1, "Name is required").max(64),
  display_name: z.string(),
  // Registered with { valueAsNumber: true }; empty → NaN fails the min check.
  quota_gib: z.number({ error: "Required" }).min(0, "Must be ≥ 0"),
  expire_at: z.string(),
  status: z.enum(["active", "disabled"])
});

type UserFormValues = z.infer<typeof userFormSchema>;

function defaults(state: Extract<UserDialogState, { mode: "create" | "edit" }>): UserFormValues {
  if (state.mode === "edit") {
    const u = state.user;
    return {
      name: u.name,
      display_name: u.display_name,
      quota_gib: bytesToGib(u.global_quota_bytes),
      expire_at: toLocalInput(u.expire_at),
      status: u.status === "disabled" ? "disabled" : "active"
    };
  }
  return { name: "", display_name: "", quota_gib: 0, expire_at: "", status: "active" };
}

export function UserFormDialog({
  request,
  state,
  onClose
}: {
  request: AdminRequest;
  state: Extract<UserDialogState, { mode: "create" | "edit" }>;
  onClose: () => void;
}) {
  const isEdit = state.mode === "edit";

  const form = useForm<UserFormValues>({
    resolver: zodResolver(userFormSchema),
    defaultValues: defaults(state)
  });
  useEffect(() => {
    form.reset(defaults(state));
  }, [form, state]);

  const mutation = useAdminMutation<UserFormValues, AdminUser>(
    request,
    (req, values) => {
      // bytesToGib rounds to 2 decimals, so converting back would drift the
      // stored byte count. Only re-send the quota when the field actually moved
      // off its initial (rounded) value; otherwise keep the exact original bytes.
      const global_quota_bytes =
        isEdit && values.quota_gib === bytesToGib(state.user.global_quota_bytes)
          ? state.user.global_quota_bytes
          : gibToBytes(values.quota_gib);
      // toLocalInput truncates to minute precision, so converting an untouched
      // value back would drift the stored RFC3339 expiry (and possibly its UTC
      // offset). Keep the exact original unless the field actually moved.
      const expire_at =
        isEdit && values.expire_at === toLocalInput(state.user.expire_at)
          ? state.user.expire_at
          : fromLocalInput(values.expire_at);
      if (!isEdit) {
        return req("/api/admin/users", {
          method: "POST",
          body: JSON.stringify({
            name: values.name.trim(),
            display_name: values.display_name.trim(),
            global_quota_bytes,
            expire_at
          })
        });
      }
      return req(`/api/admin/users/${encodeURIComponent(state.user.id)}`, {
        method: "PATCH",
        body: JSON.stringify({
          display_name: values.display_name.trim(),
          status: values.status,
          global_quota_bytes,
          expire_at
        })
      });
    },
    { onSuccess: onClose, toastError: false }
  );

  const status = form.watch("status");
  const errors = form.formState.errors;

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog size="lg" className="max-h-[calc(100dvh-2rem)] overflow-y-auto p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">
          {isEdit ? `Edit ${state.user.name}` : "Create user"}
        </Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          {isEdit
            ? "Disabling a user removes its Paths from the rendered config on the next publish."
            : "Quota and expiry are optional. Grant Paths after the user is created."}
        </Dialog.Description>

        {mutation.isError ? <Banner variant="error" title={mutation.error.message} className="mb-4" /> : null}

        <form className="flex flex-col gap-4" onSubmit={form.handleSubmit((values) => mutation.mutate(values))}>
          <Input
            label="Name"
            placeholder="alice"
            autoFocus={!isEdit}
            disabled={isEdit}
            readOnly={isEdit}
            error={errors.name?.message}
            {...form.register("name")}
          />
          <Input
            label="Display name"
            placeholder="Alice (optional)"
            error={errors.display_name?.message}
            {...form.register("display_name")}
          />
          <div className="grid grid-cols-2 gap-3">
            <Input
              label="Quota (GiB)"
              className="min-w-0 w-full"
              type="number"
              // "any": a prefilled 2-decimal GiB value (e.g. 46.57) must not fail
              // native step validation and block submits that only edit other fields.
              step="any"
              labelTooltip="Global traffic quota. 0 means unlimited."
              error={errors.quota_gib?.message}
              {...form.register("quota_gib", { valueAsNumber: true })}
            />
            <Input
              label="Expires"
              className="min-w-0 w-full"
              type="datetime-local"
              labelTooltip="Local time. Leave empty for no expiry."
              {...form.register("expire_at")}
            />
          </div>
          {isEdit ? (
            <Select
              label="Status"
              value={status}
              onValueChange={(value) => form.setValue("status", (value as UserFormValues["status"]) ?? "active")}
              items={[
                { value: "active", label: "Active" },
                { value: "disabled", label: "Disabled" }
              ]}
            />
          ) : null}

          <div className="mt-2 flex justify-end gap-2">
            <Button type="button" variant="ghost" disabled={mutation.isPending} onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={mutation.isPending}>
              {isEdit ? "Save changes" : "Create user"}
            </Button>
          </div>
        </form>
      </Dialog>
    </Dialog.Root>
  );
}

export function ManageAccessDialog({ request, user, onClose }: {
  request: AdminRequest; user: AdminUser; onClose: () => void;
}) {
  const accessQuery = useQuery({
    queryKey: adminKeys.userAccess(user.name),
    queryFn: () => request<AdminPathAccess[]>(`/api/admin/users/${encodeURIComponent(user.id)}/paths`)
  });
  const pathsQuery = useQuery({
    queryKey: adminKeys.paths,
    queryFn: () => request<AdminPath[]>("/api/admin/paths")
  });
  const groupsQuery = usePathGroups(request);
  type SelectionChange = { path_id: string; enabled: boolean };
  const mutation = useAdminMutation<SelectionChange[]>(request, (req, changes) =>
    req(`/api/admin/users/${encodeURIComponent(user.id)}/paths`, {
      method: "PATCH", body: JSON.stringify({ changes })
    }), { onSuccess: onClose, toastError: false });
  const [draft, setDraft] = useState<{ initial: Set<string>; selected: Set<string> } | null>(null);
  const [openGroups, setOpenGroups] = useState<Set<string>>(new Set());
  const pending = mutation.isPending;
  const initialSelection = draft?.initial ?? new Set((accessQuery.data ?? []).map((a) => a.path_id));
  const selected = draft?.selected ?? initialSelection;
  const paths = (pathsQuery.data ?? []).filter((p) => p.visibility === "selectable");
  const pathsByID = new Map(paths.map((p) => [p.id, p]));
  const groups = (groupsQuery.data ?? []).map((g) => ({ ...g, paths: g.path_ids.flatMap((id) => pathsByID.get(id) ? [pathsByID.get(id)!] : []) }));
  const grouped = new Set(groups.flatMap((g) => g.path_ids));
  const otherPaths = paths.filter((p) => !grouped.has(p.id));
  const sections = [...groups, ...(otherPaths.length ? [{ id: "ungrouped", name: "Other", enabled: true, paths: otherPaths }] : [])];
  const loading = accessQuery.isLoading || pathsQuery.isLoading || groupsQuery.isLoading;
  const error = accessQuery.error ?? pathsQuery.error ?? groupsQuery.error ?? mutation.error;
  const loadError = accessQuery.error ?? pathsQuery.error ?? groupsQuery.error;
  const changes = paths.filter((p) => p.enabled && selected.has(p.id) !== initialSelection.has(p.id))
    .map((p) => ({ path_id: p.id, enabled: selected.has(p.id) }));
  const dirty = changes.length > 0;
  function toggle(items: AdminPath[], checked: boolean) {
    if (pending) return;
    mutation.reset();
    setDraft((current) => {
      const initial = current?.initial ?? initialSelection;
      const next = new Set(current?.selected ?? initial);
      for (const p of items) {
        if (!p.enabled) continue;
        if (checked) next.add(p.id); else next.delete(p.id);
      }
      return { initial, selected: next };
    });
  }

  return <Dialog.Root open onOpenChange={(open) => { if (!open && !pending && !dirty) onClose(); }}>
    <Dialog size="lg" className="max-h-[calc(100dvh-2rem)] overflow-y-auto p-6">
      <Dialog.Title className="text-xl font-semibold text-kumo-default">Manage access</Dialog.Title>
      <Dialog.Description className="mb-4 text-kumo-subtle">{user.name}</Dialog.Description>
      {error ? <Banner variant="error" title={error.message} className="mb-4" /> : null}
      {loading ? <Loader size={20} /> : loadError ? null : <div className="max-h-[60dvh] overflow-y-auto rounded-md border border-kumo-line">
        {sections.map((g) => {
          const eligible = g.paths.filter((p) => p.enabled);
          const count = eligible.filter((p) => selected.has(p.id)).length;
          const open = openGroups.has(g.id);
          return <Collapsible.Root key={g.id} open={open} onOpenChange={(next) => setOpenGroups((current) => {
            const updated = new Set(current); if (next) updated.add(g.id); else updated.delete(g.id); return updated;
          })} className="border-b border-kumo-line last:border-b-0">
            <div className="flex min-w-0 items-center gap-3 px-3 py-2">
              <div className="min-w-0 grow">
                <Checkbox label={<span className="break-all">{g.name}</span>}
                  checked={eligible.length > 0 && count === eligible.length}
                  indeterminate={count > 0 && count < eligible.length}
                  disabled={pending || !g.enabled || !eligible.length}
                  onCheckedChange={() => { void toggle(eligible, count !== eligible.length); }} />
              </div>
              <span className="shrink-0 text-xs tabular-nums text-kumo-subtle" aria-label={`${count} of ${eligible.length} selected`}>{count}/{eligible.length}</span>
              <Collapsible.Trigger render={<Button variant="ghost" size="sm" shape="square" aria-label={`${open ? "Collapse" : "Expand"} ${g.name}`} />}>
                <CaretDownIcon className={`size-4 transition-transform ${open ? "rotate-180" : ""}`} />
              </Collapsible.Trigger>
            </div>
            <Collapsible.Panel>
              <div className="flex flex-col gap-3 border-t border-kumo-line py-3 pl-9 pr-3" role="group" aria-label={`${g.name} Paths`}>
                {g.paths.map((p) => <div key={p.id} className="min-w-0 shrink-0">
                  <Checkbox label={<span className="break-all">{p.display_name || `${p.proxy_name} · ${p.name}`}</span>}
                    checked={selected.has(p.id)} disabled={pending || !p.enabled}
                    onCheckedChange={(checked) => { void toggle([p], checked); }} />
                </div>)}
                {!g.paths.length ? <p className="text-sm text-kumo-subtle">No Paths</p> : null}
              </div>
            </Collapsible.Panel>
          </Collapsible.Root>;
        })}
        {!sections.length ? <p className="p-3 text-sm text-kumo-subtle">No Paths yet.</p> : null}
      </div>}
      <div className="mt-4 flex items-center justify-end gap-2">
        <Button variant="ghost" disabled={pending} onClick={onClose}>Cancel</Button>
        <Button variant="primary" loading={pending} disabled={loading || Boolean(loadError) || !dirty || pending}
          onClick={() => mutation.mutate(changes)}>Save</Button>
      </div>
    </Dialog>
  </Dialog.Root>;
}

type ConnectionProxy = UserConnectionInfo["nodes"][number]["proxies"][number];

// Credential fields differ per protocol, so emit only the ones the server
// actually populated. A VLESS-shaped payload for a Shadowsocks 2022 Path would
// be empty and unusable as a client profile.
export function proxyDetails(node: string, proxy: ConnectionProxy): string {
  return JSON.stringify(
    {
      name: proxy.name,
      proxy_name: proxy.proxy_name,
      host_tag: proxy.host_tag,
      node,
      type: proxy.type,
      server: proxy.server,
      port: proxy.server_port,
      ...(proxy.type === "shadowsocks_2022"
        ? { cipher: proxy.cipher ?? "", password: proxy.password ?? "" }
        : {
            uuid: proxy.uuid,
            flow: proxy.flow,
            server_name: proxy.server_name,
            reality_public_key: proxy.public_key,
            reality_short_id: proxy.short_id
          }),
      ...(proxy.dialer_proxy ? { dialer_proxy: proxy.dialer_proxy } : {})
    },
    null,
    2
  );
}

export function proxySummary(proxy: ConnectionProxy): string {
  const credential = proxy.type === "shadowsocks_2022" ? proxy.cipher : proxy.flow;
  return [`${proxy.server}:${proxy.server_port}`, proxy.type, credential].filter(Boolean).join(" · ");
}

export function ConnectionInfoDialog({
  request,
  user,
  onClose
}: {
  request: AdminRequest;
  user: AdminUser;
  onClose: () => void;
}) {
  const [copied, setCopied] = useState("");
  const [copyError, setCopyError] = useState("");
  const copiedResetTimer = useRef<number | null>(null);
  const encodedUser = encodeURIComponent(user.id);

  useEffect(() => {
    return () => {
      if (copiedResetTimer.current !== null) window.clearTimeout(copiedResetTimer.current);
    };
  }, []);

  const connectionQuery = useQuery({
    queryKey: adminKeys.userConnection(user.name),
    queryFn: () =>
      request<UserConnectionInfo>(`/api/admin/users/${encodedUser}/connection-info`)
  });

  async function copyText(value: string, key: string) {
    try {
      await writeClipboard(value);
      setCopyError("");
      setCopied(key);
      if (copiedResetTimer.current !== null) window.clearTimeout(copiedResetTimer.current);
      copiedResetTimer.current = window.setTimeout(() => setCopied(""), 2000);
    } catch (error) {
      setCopyError(error instanceof Error ? error.message : "Unable to copy to the clipboard.");
    }
  }

  const nodes = connectionQuery.data?.nodes ?? [];
  const proxyCount = nodes.reduce((total, node) => total + node.proxies.length, 0);
  const requestError = connectionQuery.error;

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog size="xl" className="max-h-[calc(100dvh-2rem)] overflow-y-auto p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">
          Connection info
        </Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          Connection details for{" "}
          <span className="font-medium text-kumo-default">{user.name}</span>.
        </Dialog.Description>

        {requestError ? (
          <Banner
            variant="error"
            title={requestError instanceof Error ? requestError.message : "Request failed."}
            className="mb-4"
          />
        ) : null}
        {copyError ? <Banner variant="error" title={copyError} className="mb-4" /> : null}

        <section className="mt-4">
          <div className="mb-3 flex items-end justify-between gap-3">
            <div>
              <h3 className="font-semibold text-kumo-default">Connection profiles</h3>
              <p className="text-sm text-kumo-subtle">
                {proxyCount} {proxyCount === 1 ? "proxy" : "proxies"} across {nodes.length}{" "}
                {nodes.length === 1 ? "node" : "nodes"}
              </p>
            </div>
          </div>

          {connectionQuery.isLoading ? (
            <div className="flex min-h-32 items-center justify-center">
              <Loader size={20} />
            </div>
          ) : proxyCount > 0 ? (
            <div className="flex max-h-80 flex-col gap-3 overflow-y-auto">
              {nodes.map((node) =>
                node.proxies.length > 0 ? (
                  <div
                    key={node.node}
                    className="rounded-lg border border-kumo-line bg-kumo-canvas p-3"
                  >
                    <h4 className="mb-2 text-sm font-semibold text-kumo-default">{node.node}</h4>
                    <div className="flex flex-col gap-2">
                      {node.proxies.map((proxy) => {
                        const key = `${node.node}/${proxy.name}/${proxy.server}`;
                        return (
                          <div
                            key={key}
                            className="flex flex-col gap-2 rounded-md bg-kumo-base px-3 py-2 sm:flex-row sm:items-center sm:justify-between"
                          >
                            <div className="min-w-0">
                              <div className="truncate text-sm font-medium text-kumo-default">
                                {proxy.name}
                              </div>
                              <div className="truncate font-mono text-xs text-kumo-subtle">
                                {proxySummary(proxy)}
                              </div>
                            </div>
                            <Button
                              variant="secondary"
                              size="sm"
                              icon={CopyIcon}
                              onClick={() =>
                                void copyText(proxyDetails(node.node, proxy), `proxy:${key}`)
                              }
                            >
                              {copied === `proxy:${key}` ? "Copied" : "Copy details"}
                            </Button>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                ) : null
              )}
            </div>
          ) : (
            <div className="flex min-h-28 items-center justify-center rounded-lg border border-kumo-line bg-kumo-canvas text-sm text-kumo-subtle">
              No active connection profiles.
            </div>
          )}
        </section>

        <div className="mt-2 flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            Done
          </Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
