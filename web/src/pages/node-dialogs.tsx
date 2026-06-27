import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { CopyIcon } from "@phosphor-icons/react";
import { Banner, Button, Dialog, Input } from "@cloudflare/kumo";

import type { AdminNode, AdminNodeBootstrap } from "../types";
import type { AdminRequest } from "@/publish/publish-status";
import { useAdminMutation } from "@/admin/use-admin-mutation";

export type NodeDialogState =
  | { mode: "enroll" }
  | { mode: "edit"; node: AdminNode }
  | { mode: "delete"; node: AdminNode }
  | null;

function CopyField({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex flex-col gap-1">
      <span className="text-sm font-medium text-kumo-default">{label}</span>
      <div className="flex items-stretch gap-2">
        <code className="min-w-0 flex-1 truncate rounded-md border border-kumo-line bg-kumo-canvas px-3 py-2 font-mono text-xs text-kumo-subtle">
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
          <div className="flex flex-col gap-4">
            <CopyField label="Bootstrap string" value={result.bootstrap_string} />
            <CopyField label="Install script URL" value={result.install_script_url} />
            <div className="mt-2 flex justify-end">
              <Button onClick={onClose}>Done</Button>
            </div>
          </div>
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

// Status is owned by the Disable/Enable toggle and Decommission action, not this
// form — including it here would silently promote a pending/degraded node to
// active on an unrelated host/URL edit. The PATCH omits status, so the server
// preserves it.
const editSchema = z.object({
  public_host: z.string(),
  api_base_url: z.string()
});

type EditValues = z.infer<typeof editSchema>;

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
    defaultValues: {
      public_host: node.public_host,
      api_base_url: node.api_base_url
    }
  });
  useEffect(() => {
    form.reset({
      public_host: node.public_host,
      api_base_url: node.api_base_url
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [node.id]);

  const mutation = useAdminMutation<EditValues, AdminNode>(
    request,
    (req, values) =>
      req(`/api/admin/nodes/${encodeURIComponent(node.name)}`, {
        method: "PATCH",
        body: JSON.stringify(values)
      }),
    { onSuccess: onClose }
  );

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog size="base" className="p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">Edit {node.name}</Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          Update the node's public host and API URL. Use Disable or Decommission to change its status.
        </Dialog.Description>

        {mutation.isError ? <Banner variant="error" title={mutation.error.message} className="mb-4" /> : null}

        <form className="flex flex-col gap-4" onSubmit={form.handleSubmit((values) => mutation.mutate(values))}>
          <Input label="Public host" placeholder="203.0.113.10" {...form.register("public_host")} />
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
        <Dialog.Title className="text-xl font-semibold text-kumo-default">Decommission node</Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          Decommission <span className="font-medium text-kumo-default">{node.name}</span>? This disables it
          and revokes its agent token, cutting the daemon off entirely. The record is kept for history; use{" "}
          <span className="font-medium text-kumo-default">Disable</span> instead to only pause serving.
        </Dialog.Description>

        {mutation.isError ? <Banner variant="error" title={mutation.error.message} className="mb-4" /> : null}

        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="destructive" loading={mutation.isPending} onClick={() => mutation.mutate()}>
            Decommission
          </Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
