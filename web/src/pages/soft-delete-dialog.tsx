import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Banner, Button, Collapsible, Dialog } from "@cloudflare/kumo";

import type { AdminRequest } from "@/admin/api";
import type { DeletionImpact, DeletionResourceKind } from "@/types";
import { useAdminMutation } from "@/admin/use-admin-mutation";

export function SoftDeleteDialog({
  request,
  title,
  description,
  endpoint,
  resource,
  confirmLabel = "Delete",
  onClose
}: {
  request: AdminRequest;
  title: string;
  description: ReactNode;
  endpoint: string;
  resource?: { kind: DeletionResourceKind; id: string };
  confirmLabel?: string;
  onClose: () => void;
}) {
  const impact = useQuery<DeletionImpact>({
    queryKey: ["admin", "deletion-impact", resource?.kind, resource?.id],
    queryFn: () => request(`/api/admin/deletion-impact?kind=${resource!.kind}&id=${encodeURIComponent(resource!.id)}`),
    enabled: Boolean(resource),
    staleTime: 0
  });
  const groups: Record<string, NonNullable<typeof impact.data>["items"]> = {};
  for (const item of impact.data?.items ?? []) (groups[item.kind] ??= []).push(item);
  const mutation = useAdminMutation<void, unknown>(
    request,
    async (req) => {
      if (resource) {
        const fresh = await impact.refetch();
        if (fresh.error) throw fresh.error;
        if (fresh.data?.blocked) throw new Error(fresh.data.blocked);
        if (JSON.stringify(fresh.data?.items) !== JSON.stringify(impact.data?.items)) {
          throw new Error("Affected resources changed. Review the updated list before deleting.");
        }
      }
      return req(endpoint, { method: "DELETE" });
    },
    { onSuccess: onClose, toastError: false }
  );

  return (
    <Dialog.Root open onOpenChange={(open) => (open || mutation.isPending ? undefined : onClose())}>
      <Dialog size={resource ? "base" : "sm"} className="max-h-[calc(100dvh-2rem)] overflow-y-auto p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">{title}</Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">{description}</Dialog.Description>
        {resource ? (
          <section aria-label="Deletion impact" className="mb-4 min-w-0">
            <h3 className="mb-2 text-sm font-medium text-kumo-default">Affected resources</h3>
            {impact.isPending ? <p className="text-sm text-kumo-subtle">Checking dependencies…</p> : null}
            {impact.isError ? <Banner variant="error" title="Unable to check dependencies"><Button variant="secondary" size="sm" onClick={() => void impact.refetch()}>Retry</Button></Banner> : null}
            {impact.data?.blocked ? <Banner variant="error" title={impact.data.blocked} className="mb-2" /> : null}
            {impact.data && !impact.data.items.length ? <p className="text-sm text-kumo-subtle">No other resources are affected.</p> : null}
            <div className="max-h-[45dvh] overflow-y-auto rounded-md border border-kumo-fill">
              {Object.entries(groups).map(([kind, items]) => (
                <Collapsible.Root key={kind} defaultOpen className="border-b border-kumo-fill last:border-b-0">
                  <Collapsible.DefaultTrigger className="w-full px-3 py-2 text-sm">{kind} · {items.length}</Collapsible.DefaultTrigger>
                  <Collapsible.DefaultPanel className="px-3 pb-2">
                    <ul className="space-y-2">
                      {items.map((item) => <li key={item.id} className="flex min-w-0 flex-wrap items-start justify-between gap-x-3 gap-y-1 text-sm">
                        <span className="min-w-0 flex-1 break-words [overflow-wrap:anywhere]">{item.name}</span>
                        <span className="shrink-0 text-kumo-subtle">{item.effect}</span>
                      </li>)}
                    </ul>
                  </Collapsible.DefaultPanel>
                </Collapsible.Root>
              ))}
            </div>
            <p className="mt-2 text-xs text-kumo-subtle">Traffic history is retained.</p>
          </section>
        ) : null}
        {mutation.isError ? <Banner variant="error" title={mutation.error.message} className="mb-4" /> : null}
        <div className="sticky bottom-0 flex justify-end gap-2 bg-kumo-base pt-2">
          <Button variant="ghost" disabled={mutation.isPending} onClick={onClose}>
            Cancel
          </Button>
          <Button variant="destructive" loading={mutation.isPending} disabled={Boolean(resource) && (impact.isPending || impact.isError || Boolean(impact.data?.blocked))} onClick={() => mutation.mutate()}>
            {confirmLabel}
          </Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
