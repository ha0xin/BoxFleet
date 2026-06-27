import { useEffect, useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { PlusIcon, TrashIcon } from "@phosphor-icons/react";
import { Banner, Button, Dialog, Input, Loader, Select } from "@cloudflare/kumo";

import type { AdminProxiesResponse, AdminProxy, AdminProxyAccess, AdminUser } from "../types";
import type { AdminRequest } from "@/publish/publish-status";
import { useAdminMutation } from "@/admin/use-admin-mutation";
import { useQuery } from "@tanstack/react-query";

export type UserDialogState =
  | { mode: "create" }
  | { mode: "edit"; user: AdminUser }
  | { mode: "access"; user: AdminUser }
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
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state.mode, isEdit ? state.user.id : "create"]);

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
      return req(`/api/admin/users/${encodeURIComponent(state.user.name)}`, {
        method: "PATCH",
        body: JSON.stringify({
          display_name: values.display_name.trim(),
          status: values.status,
          global_quota_bytes,
          expire_at
        })
      });
    },
    { onSuccess: onClose }
  );

  const status = form.watch("status");
  const errors = form.formState.errors;

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog size="base" className="p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">
          {isEdit ? `Edit ${state.user.name}` : "Create user"}
        </Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          {isEdit
            ? "Disabling a user removes its accesses from the rendered config on the next publish."
            : "Quota and expiry are optional. Issue proxy access after the user is created."}
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
            <Button type="button" variant="ghost" onClick={onClose}>
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

function AccessRow({
  request,
  user,
  access
}: {
  request: AdminRequest;
  user: AdminUser;
  access: AdminProxyAccess;
}) {
  const revoke = useAdminMutation<void, unknown>(request, (req) =>
    req(
      `/api/admin/users/${encodeURIComponent(user.name)}/proxies/${encodeURIComponent(access.node_name)}/${encodeURIComponent(access.proxy_name)}`,
      { method: "DELETE" }
    )
  );

  return (
    <div className="flex items-center justify-between gap-3 rounded-md border border-kumo-line bg-kumo-canvas px-3 py-2">
      <div className="min-w-0">
        <div className="truncate text-sm font-medium text-kumo-default">
          {access.node_name} / {access.proxy_name}
        </div>
        <div className="truncate text-xs text-kumo-subtle">
          {access.protocol} · {access.listen_port}
        </div>
      </div>
      <Button
        variant="ghost"
        size="sm"
        shape="square"
        aria-label={`Revoke ${access.proxy_name}`}
        loading={revoke.isPending}
        onClick={() => revoke.mutate()}
      >
        <TrashIcon className="size-4 text-kumo-danger" />
      </Button>
    </div>
  );
}

export function ManageAccessDialog({
  request,
  user,
  onClose
}: {
  request: AdminRequest;
  user: AdminUser;
  onClose: () => void;
}) {
  const [selected, setSelected] = useState<string>("");

  const accessQuery = useQuery({
    queryKey: ["admin", "user-access", user.name],
    queryFn: () => request<AdminProxyAccess[]>(`/api/admin/users/${encodeURIComponent(user.name)}/proxies`)
  });
  const proxiesQuery = useQuery({
    queryKey: ["admin", "proxies-all"],
    queryFn: () => request<AdminProxiesResponse>("/api/admin/proxies?limit=500")
  });

  const accesses = accessQuery.data ?? [];
  const existing = useMemo(
    () => new Set(accesses.map((a) => `${a.node_name} ${a.proxy_name}`)),
    [accesses]
  );
  const available = useMemo<AdminProxy[]>(
    () =>
      (proxiesQuery.data?.proxies ?? []).filter(
        (p) => p.enabled && !existing.has(`${p.node_name} ${p.name}`)
      ),
    [proxiesQuery.data, existing]
  );
  const items = available.map((p) => ({ value: p.id, label: `${p.node_name} / ${p.name}` }));
  const selectedProxy = available.find((p) => p.id === selected);

  const issue = useAdminMutation<AdminProxy, unknown>(
    request,
    (req, proxy) =>
      req(`/api/admin/users/${encodeURIComponent(user.name)}/proxies`, {
        method: "POST",
        body: JSON.stringify({ node_name: proxy.node_name, proxy_name: proxy.name })
      }),
    { onSuccess: () => setSelected("") }
  );

  const loading = accessQuery.isLoading || proxiesQuery.isLoading;

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog size="base" className="p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">Manage access</Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          Issue or revoke proxy access for <span className="font-medium text-kumo-default">{user.name}</span>.
          Changes alter the rendered config on the next publish.
        </Dialog.Description>

        {issue.isError ? <Banner variant="error" title={issue.error.message} className="mb-4" /> : null}

        <div className="flex items-end gap-2">
          <Select
            label="Add proxy"
            className="flex-1"
            value={selected || null}
            onValueChange={(value) => setSelected((value as string) ?? "")}
            items={items}
            placeholder={items.length > 0 ? "Select a proxy" : "No more proxies available"}
            disabled={items.length === 0}
          />
          <Button
            icon={PlusIcon}
            loading={issue.isPending}
            disabled={!selectedProxy}
            onClick={() => selectedProxy && issue.mutate(selectedProxy)}
          >
            Issue
          </Button>
        </div>

        <div className="mt-4 flex max-h-72 flex-col gap-2 overflow-y-auto">
          {loading ? (
            <div className="flex min-h-24 items-center justify-center">
              <Loader size={20} />
            </div>
          ) : accesses.length > 0 ? (
            accesses.map((access) => (
              <AccessRow key={access.id} request={request} user={user} access={access} />
            ))
          ) : (
            <div className="flex min-h-24 items-center justify-center text-sm text-kumo-subtle">
              No access issued yet.
            </div>
          )}
        </div>

        <div className="mt-4 flex justify-end">
          <Button variant="secondary" onClick={onClose}>
            Done
          </Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
