import { useEffect, useState } from "react";
import { useFieldArray, useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { CopyIcon, PlusIcon, TrashIcon } from "@phosphor-icons/react";
import { Banner, Button, Dialog, Input, Switch } from "@cloudflare/kumo";

import type { AdminNode, AdminNodeBootstrap } from "../types";
import type { AdminRequest } from "@/publish/publish-status";
import { useAdminMutation } from "@/admin/use-admin-mutation";

export type NodeDialogState =
  | { mode: "enroll" }
  | { mode: "edit"; node: AdminNode }
  | { mode: "delete"; node: AdminNode }
  | { mode: "reenroll"; node: AdminNode }
  | null;

// Single-quote a value for safe inclusion in a /bin/sh command (escape embedded
// single quotes the POSIX way: close, add an escaped quote, reopen).
function shellQuote(value: string): string {
  return `'${value.replace(/'/g, "'\\''")}'`;
}

// The download-script and the bootstrap token belong in one runnable command so
// the operator can paste a single line on the node. Download to a temp file then
// run it (lets the script be re-inspected/re-run) rather than piping curl|sh.
function installCommand(scriptUrl: string, bootstrap: string): string {
  return `curl -fsSL ${shellQuote(scriptUrl)} -o /tmp/boxfleet-install.sh && sudo sh /tmp/boxfleet-install.sh ${shellQuote(bootstrap)}`;
}

function CopyField({ label, value, wrap = false }: { label: string; value: string; wrap?: boolean }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex flex-col gap-1">
      <span className="text-sm font-medium text-kumo-default">{label}</span>
      <div className="flex items-stretch gap-2">
        <code
          className={`min-w-0 flex-1 rounded-md border border-kumo-line bg-kumo-canvas px-3 py-2 font-mono text-xs text-kumo-subtle ${
            wrap ? "whitespace-pre-wrap break-all" : "truncate"
          }`}
        >
          {value}
        </code>
        <Button
          variant="secondary"
          size="sm"
          shape="square"
          aria-label={`Copy ${label}`}
          onClick={() => {
            void navigator.clipboard?.writeText(value);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1500);
          }}
        >
          <CopyIcon className="size-4" />
        </Button>
      </div>
      {copied ? <span className="text-xs text-kumo-success">Copied</span> : null}
    </div>
  );
}

// Shared success view for enroll + re-enroll: one copy-paste install command.
function BootstrapResult({ result, onClose }: { result: AdminNodeBootstrap; onClose: () => void }) {
  return (
    <div className="flex flex-col gap-4">
      <CopyField
        label="Install command"
        value={installCommand(result.install_script_url, result.bootstrap_string)}
        wrap
      />
      <div className="mt-2 flex justify-end">
        <Button onClick={onClose}>Done</Button>
      </div>
    </div>
  );
}

const enrollSchema = z.object({
  name: z.string().min(1, "Name is required").max(64),
  public_host: z.string()
});

type EnrollValues = z.infer<typeof enrollSchema>;

export function EnrollNodeDialog({ request, onClose }: { request: AdminRequest; onClose: () => void }) {
  const form = useForm<EnrollValues>({
    resolver: zodResolver(enrollSchema),
    defaultValues: { name: "", public_host: "" }
  });
  const [result, setResult] = useState<AdminNodeBootstrap | null>(null);

  const mutation = useAdminMutation<EnrollValues, AdminNodeBootstrap>(
    request,
    (req, values) =>
      req("/api/admin/nodes/bootstrap", {
        method: "POST",
        body: JSON.stringify({ name: values.name.trim(), public_host: values.public_host.trim() || undefined })
      }),
    { onSuccess: (data) => setResult(data) }
  );

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog size="lg" className="p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">Enroll node</Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          {result
            ? "Run the bootstrap on the node. It appears as pending until the agent reports in."
            : "Create a node record and generate its one-time bootstrap string."}
        </Dialog.Description>

        {mutation.isError ? <Banner variant="error" title={mutation.error.message} className="mb-4" /> : null}

        {result ? (
          <BootstrapResult result={result} onClose={onClose} />
        ) : (
          <form
            className="flex flex-col gap-4"
            onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
          >
            <Input
              label="Node name"
              placeholder="tokyo"
              autoFocus
              error={form.formState.errors.name?.message}
              {...form.register("name")}
            />
            <Input
              label="Public host"
              placeholder="203.0.113.10 (optional)"
              labelTooltip="Public IP or hostname clients connect to. Can be set later."
              {...form.register("public_host")}
            />
            <div className="mt-2 flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={onClose}>
                Cancel
              </Button>
              <Button type="submit" loading={mutation.isPending}>
                Generate bootstrap
              </Button>
            </div>
          </form>
        )}
      </Dialog>
    </Dialog.Root>
  );
}

// Re-show the install command for an existing node. The original bootstrap token
// is unrecoverable (only its hash is stored), so the server rotates the token on
// re-enroll. Offered for a pending node (bootstrap lost before first check-in) or
// a decommissioned one being restored — not for an active node serving traffic.
export function ReenrollNodeDialog({
  request,
  node,
  onClose
}: {
  request: AdminRequest;
  node: AdminNode;
  onClose: () => void;
}) {
  const [result, setResult] = useState<AdminNodeBootstrap | null>(null);
  const isRestore = node.status === "disabled";

  const mutation = useAdminMutation<void, AdminNodeBootstrap>(
    request,
    (req) => req(`/api/admin/nodes/${encodeURIComponent(node.name)}/reenroll`, { method: "POST" }),
    { onSuccess: (data) => setResult(data) }
  );

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog size="lg" className="p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">
          {isRestore ? `Re-enroll ${node.name}` : `Install command for ${node.name}`}
        </Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          {result
            ? "Run the command on the node. It appears as pending until the agent reports in."
            : isRestore
              ? "Issue a fresh agent token and bootstrap string to bring this decommissioned node back online. Its old token stays revoked."
              : "Re-issue this node's install command. A fresh agent token is generated and any previous token is revoked."}
        </Dialog.Description>

        {mutation.isError ? <Banner variant="error" title={mutation.error.message} className="mb-4" /> : null}

        {result ? (
          <BootstrapResult result={result} onClose={onClose} />
        ) : (
          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button loading={mutation.isPending} onClick={() => mutation.mutate()}>
              {isRestore ? "Re-enroll" : "Generate install command"}
            </Button>
          </div>
        )}
      </Dialog>
    </Dialog.Root>
  );
}

// Status is owned by the Disable/Enable toggle and Decommission action, not this
// form — including it here would silently promote a pending/degraded node to
// active on an unrelated host/URL edit. The PATCH omits status, so the server
// preserves it.
const editSchema = z.object({
  name: z.string().trim().min(1, "Name is required").max(64, "Use at most 64 characters"),
  // A node may publish several addresses (domain, IPv4, IPv6). The first is the
  // primary (mirrored to public_host server-side); each "selected" host yields a
  // client profile. Require at least one non-empty host and one selected.
  hosts: z
    .array(z.object({ host: z.string(), tag: z.string(), selected: z.boolean() }))
    .refine((hosts) => hosts.some((h) => h.host.trim() !== ""), {
      message: "At least one host is required"
    })
    .refine((hosts) => hosts.some((h) => h.host.trim() !== "" && h.selected), {
      message: "Select at least one host to generate a client profile"
    })
    .superRefine((hosts, context) => {
      const tags = new Map<string, number>();
      hosts.forEach((host, index) => {
        if (host.host.trim() === "") return;
        const tag = host.tag.trim();
        if (index > 0 && tag === "") {
          context.addIssue({
            code: "custom",
            message: "Tag is required for an additional host",
            path: [index, "tag"]
          });
          return;
        }
        if ([...tag].length > 32) {
          context.addIssue({
            code: "custom",
            message: "Use at most 32 characters",
            path: [index, "tag"]
          });
        }
        if (/\p{Cc}/u.test(tag)) {
          context.addIssue({
            code: "custom",
            message: "Control characters are not allowed",
            path: [index, "tag"]
          });
        }
        if (tag === "") return;
        const normalized = tag.toLowerCase();
        const duplicate = tags.get(normalized);
        if (duplicate !== undefined) {
          context.addIssue({
            code: "custom",
            message: `Tag duplicates host ${duplicate + 1}`,
            path: [index, "tag"]
          });
        } else {
          tags.set(normalized, index);
        }
      });
    }),
  api_base_url: z.string()
});

type EditValues = z.infer<typeof editSchema>;

function editDefaults(node: AdminNode): EditValues {
  const hosts =
    node.hosts && node.hosts.length > 0
      ? node.hosts.map((h) => ({ host: h.host, tag: h.tag ?? "", selected: h.selected }))
      : [{ host: node.public_host, tag: "", selected: true }];
  return { name: node.name, hosts, api_base_url: node.api_base_url };
}

export function EditNodeDialog({
  request,
  node,
  onClose
}: {
  request: AdminRequest;
  node: AdminNode;
  onClose: () => void;
}) {
  const form = useForm<EditValues>({
    resolver: zodResolver(editSchema),
    defaultValues: editDefaults(node)
  });
  const { fields, append, remove } = useFieldArray({ control: form.control, name: "hosts" });
  useEffect(() => {
    form.reset(editDefaults(node));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [node.id]);

  const mutation = useAdminMutation<EditValues, AdminNode>(
    request,
    (req, values) => {
      const hosts = values.hosts
        .map((h) => ({ host: h.host.trim(), tag: h.tag.trim(), selected: h.selected }))
        .filter((h) => h.host !== "");
      return req(`/api/admin/nodes/${encodeURIComponent(node.name)}`, {
        method: "PATCH",
        body: JSON.stringify({ name: values.name.trim(), hosts, api_base_url: values.api_base_url.trim() })
      });
    },
    { onSuccess: onClose }
  );

  const hostsErrors = form.formState.errors.hosts;
  const hostsError = hostsErrors?.message ?? hostsErrors?.root?.message;

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog size="base" className="p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">Edit {node.name}</Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          Update the node's name, hosts, and API URL. Use Disable or Decommission to change its status.
        </Dialog.Description>

        {mutation.isError ? <Banner variant="error" title={mutation.error.message} className="mb-4" /> : null}

        <form className="flex flex-col gap-4" onSubmit={form.handleSubmit((values) => mutation.mutate(values))}>
          <Input
            label="Node name"
            error={form.formState.errors.name?.message}
            {...form.register("name")}
          />
          <div className="flex flex-col gap-2">
            <span className="text-sm font-medium text-kumo-default">Hosts</span>
            <span className="text-xs text-kumo-subtle">
              The first host is primary. Additional hosts require a unique tag. Each host with “Profile” on
              generates a client connection profile.
            </span>
            {fields.map((field, index) => (
              <div key={field.id} className="grid grid-cols-[minmax(0,2fr)_minmax(7rem,1fr)_auto_auto] items-start gap-2">
                <Input
                  placeholder="203.0.113.10 · example.com · 2606:4700::1"
                  error={form.formState.errors.hosts?.[index]?.host?.message}
                  {...form.register(`hosts.${index}.host`)}
                />
                <Input
                  placeholder={index === 0 ? "Tag (optional)" : "Tag"}
                  error={form.formState.errors.hosts?.[index]?.tag?.message}
                  {...form.register(`hosts.${index}.tag`)}
                />
                <Switch
                  controlFirst={false}
                  label="Profile"
                  checked={form.watch(`hosts.${index}.selected`)}
                  onCheckedChange={(value) => form.setValue(`hosts.${index}.selected`, Boolean(value))}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  shape="square"
                  aria-label="Remove host"
                  disabled={fields.length === 1}
                  onClick={() => remove(index)}
                >
                  <TrashIcon className="size-4 text-kumo-danger" />
                </Button>
              </div>
            ))}
            <div>
              <Button
                type="button"
                variant="secondary"
                size="sm"
                icon={PlusIcon}
                onClick={() => append({ host: "", tag: "", selected: false })}
              >
                Add host
              </Button>
            </div>
            {hostsError ? <span className="text-xs text-kumo-danger">{hostsError}</span> : null}
          </div>
          <Input label="API base URL" placeholder="https://203.0.113.10:18080" {...form.register("api_base_url")} />
          <div className="mt-2 flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={mutation.isPending}>
              Save changes
            </Button>
          </div>
        </form>
      </Dialog>
    </Dialog.Root>
  );
}

export function DeleteNodeDialog({
  request,
  node,
  onClose
}: {
  request: AdminRequest;
  node: AdminNode;
  onClose: () => void;
}) {
  const mutation = useAdminMutation<void, unknown>(
    request,
    (req) => req(`/api/admin/nodes/${encodeURIComponent(node.name)}`, { method: "DELETE" }),
    { onSuccess: onClose }
  );

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog size="sm" className="p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">Delete node</Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          Delete <span className="font-medium text-kumo-default">{node.name}</span>? This disables it,
          revokes its agent token, and hides it from the default inventory. You can restore it from the Deleted
          filter. Use <span className="font-medium text-kumo-default">Disable</span> instead to only pause serving.
        </Dialog.Description>

        {mutation.isError ? <Banner variant="error" title={mutation.error.message} className="mb-4" /> : null}

        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="destructive" loading={mutation.isPending} onClick={() => mutation.mutate()}>
            Delete
          </Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
