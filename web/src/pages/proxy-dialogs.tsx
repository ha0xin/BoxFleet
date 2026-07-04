import { useEffect } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useQuery } from "@tanstack/react-query";
import { Banner, Button, Dialog, Input, Select, Switch } from "@cloudflare/kumo";

import type { AdminNode, AdminProxy } from "../types";
import type { AdminRequest } from "@/publish/publish-status";
import { useAdminMutation } from "@/admin/use-admin-mutation";

export type ProxyDialogState =
  | { mode: "create" }
  | { mode: "edit"; proxy: AdminProxy }
  | null;

const proxyFormSchema = z.object({
  node_name: z.string().min(1, "Select a node"),
  name: z.string().min(1, "Name is required"),
  // Inputs registered with { valueAsNumber: true }, so these are already numbers
  // (empty → NaN, which fails the min check).
  listen_port: z.number({ error: "Required" }).int().min(1, "1-65535").max(65535, "1-65535"),
  server_name: z.string(),
  short_id: z
    .string()
    .trim()
    .refine((value) => /^(?:[0-9a-fA-F]{2}){0,4}$/.test(value), {
      message: "Use 0–8 hexadecimal characters with an even length"
    }),
  // Backend rewrites 0 → 1.0, so reject 0 here to avoid a silent mismatch.
  traffic_multiplier: z.number({ error: "Required" }).gt(0, "Must be greater than 0"),
  enabled: z.boolean()
});

type ProxyFormValues = z.infer<typeof proxyFormSchema>;

function parseServerName(settingsJSON: string): string {
  try {
    const parsed = JSON.parse(settingsJSON) as { server_name?: string };
    return parsed?.server_name ?? "";
  } catch {
    return "";
  }
}

function parseShortID(proxy: AdminProxy): string {
  if (typeof proxy.short_id === "string") return proxy.short_id;
  try {
    const parsed = JSON.parse(proxy.settings_json) as { short_id?: string };
    return parsed?.short_id ?? "";
  } catch {
    return "";
  }
}

// Preserve the existing settings (Reality keys, short_id, handshake) and only
// override server_name. Returns a JSON string for the PATCH payload.
function mergeServerName(settingsJSON: string, serverName: string): string {
  let settings: Record<string, unknown> = {};
  try {
    const parsed = JSON.parse(settingsJSON || "{}");
    // typeof [] === "object", so guard against arrays: assigning server_name to
    // an array would be dropped by JSON.stringify and silently lose the SNI.
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      settings = parsed as Record<string, unknown>;
    }
  } catch {
    settings = {};
  }
  if (serverName) {
    settings.server_name = serverName;
  } else {
    delete settings.server_name;
  }
  return JSON.stringify(settings);
}

function defaults(state: Exclude<ProxyDialogState, null>): ProxyFormValues {
  if (state.mode === "edit") {
    const p = state.proxy;
    return {
      node_name: p.node_name,
      name: p.name,
      listen_port: p.listen_port,
      server_name: parseServerName(p.settings_json),
      short_id: parseShortID(p),
      traffic_multiplier: p.traffic_multiplier,
      enabled: p.enabled
    };
  }
  return {
    node_name: "",
    name: "",
    listen_port: 443,
    server_name: "",
    short_id: "",
    traffic_multiplier: 1,
    enabled: true
  };
}

export function ProxyFormDialog({
  request,
  state,
  onClose
}: {
  request: AdminRequest;
  state: Extract<ProxyDialogState, { mode: "create" | "edit" }>;
  onClose: () => void;
}) {
  const isEdit = state.mode === "edit";

  const form = useForm<ProxyFormValues>({
    resolver: zodResolver(proxyFormSchema),
    defaultValues: defaults(state)
  });
  // Reset whenever the dialog target changes.
  useEffect(() => {
    form.reset(defaults(state));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state.mode, isEdit ? state.proxy.id : "create"]);

  const nodesQuery = useQuery({
    queryKey: ["admin", "nodes-all"],
    queryFn: () => request<AdminNode[]>("/api/admin/nodes"),
    enabled: !isEdit
  });
  const nodeItems = (nodesQuery.data ?? []).map((n) => ({ value: n.name, label: n.name }));

  // Default the node select to the first node once the list loads.
  const nodeName = form.watch("node_name");
  useEffect(() => {
    if (!isEdit && !nodeName && nodeItems.length > 0) {
      form.setValue("node_name", nodeItems[0].value, { shouldValidate: true });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isEdit, nodeName, nodeItems.length]);

  const mutation = useAdminMutation<ProxyFormValues, AdminProxy>(
    request,
    (req, values) => {
      const sni = values.server_name.trim();
      if (!isEdit) {
        // Server generates Reality keys; only seed the SNI when given.
        const settings_json = sni ? JSON.stringify({ server_name: sni }) : undefined;
        return req(`/api/admin/nodes/${encodeURIComponent(values.node_name)}/proxies`, {
          method: "POST",
          body: JSON.stringify({
            name: values.name.trim(),
            short_id: values.short_id.trim().toLowerCase(),
            listen_port: values.listen_port,
            traffic_multiplier: values.traffic_multiplier,
            enabled: values.enabled,
            settings_json
          })
        });
      }
      // Merge the edited SNI into the existing settings so the server keeps the
      // current Reality key pair / short_id instead of regenerating them.
      const settings_json = mergeServerName(state.proxy.settings_json, sni);
      return req(
        `/api/admin/nodes/${encodeURIComponent(values.node_name)}/proxies/${encodeURIComponent(state.proxy.name)}`,
        {
          method: "PATCH",
          body: JSON.stringify({
            name: values.name.trim(),
            short_id: values.short_id.trim().toLowerCase(),
            listen_port: values.listen_port,
            traffic_multiplier: values.traffic_multiplier,
            enabled: values.enabled,
            settings_json
          })
        }
      );
    },
    { onSuccess: onClose }
  );

  const enabled = form.watch("enabled");
  const errors = form.formState.errors;

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog size="base" className="p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">
          {isEdit ? `Edit ${state.proxy.name}` : "Create proxy"}
        </Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          VLESS-Reality inbound. Reality keys are generated server-side. Name and Reality changes require
          publishing the node configuration.
        </Dialog.Description>

        {mutation.isError ? (
          <Banner variant="error" title={mutation.error.message} className="mb-4" />
        ) : null}

        <form
          className="flex flex-col gap-4"
          onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
        >
          {isEdit ? (
            <Input label="Node" value={state.proxy.node_name} disabled readOnly />
          ) : (
            <Select
              label="Node"
              value={nodeName || null}
              onValueChange={(value) => form.setValue("node_name", (value as string) ?? "", { shouldValidate: true })}
              items={nodeItems}
              error={errors.node_name?.message}
            />
          )}

          <Input
            label="Name"
            placeholder="tokyo-reality"
            error={errors.name?.message}
            {...form.register("name")}
          />

          <div className="grid grid-cols-2 gap-3">
            <Input
              label="Listen port"
              type="number"
              error={errors.listen_port?.message}
              {...form.register("listen_port", { valueAsNumber: true })}
            />
            <Input
              label="Traffic multiplier"
              type="number"
              // "any": existing arbitrary multipliers (e.g. 1.25) must not fail
              // native step validation and block edits to other fields.
              step="any"
              error={errors.traffic_multiplier?.message}
              {...form.register("traffic_multiplier", { valueAsNumber: true })}
            />
          </div>

          <Input
            label="Reality SNI"
            placeholder="www.amazon.com"
            labelTooltip="Server name presented in the Reality handshake. Defaults to www.amazon.com."
            {...form.register("server_name")}
          />

          {isEdit ? (
            <Input
              label="Reality short ID"
              placeholder="01234567 (optional)"
              labelTooltip="Empty or an even-length hexadecimal value of at most 8 characters."
              error={errors.short_id?.message}
              {...form.register("short_id")}
            />
          ) : null}

          <Switch
            label="Enabled"
            controlFirst={false}
            checked={enabled}
            onCheckedChange={(value) => form.setValue("enabled", Boolean(value))}
          />

          <div className="mt-2 flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={mutation.isPending}>
              {isEdit ? "Save changes" : "Create proxy"}
            </Button>
          </div>
        </form>
      </Dialog>
    </Dialog.Root>
  );
}
