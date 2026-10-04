import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Banner, Button, Dialog, Table } from "@cloudflare/kumo";

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
            {impact.data?.items.length ? (
              <div className="max-h-[45dvh] overflow-y-auto rounded-lg border border-kumo-line">
                <Table layout="fixed">
                  <colgroup><col /><col className="w-28" /></colgroup>
                  <Table.Header variant="compact" className="sticky top-0 z-10">
                    <Table.Row>
                      <Table.Head>Resource</Table.Head>
                      <Table.Head>Change</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {impact.data.items.map((item) => (
                      <Table.Row key={`${item.kind}:${item.id}`}>
                        <Table.Cell className="whitespace-normal [overflow-wrap:anywhere]">
                          <div className="text-sm text-kumo-default">{item.name}</div>
                          <div className="mt-0.5 text-xs text-kumo-subtle">{item.kind}</div>
                        </Table.Cell>
                        <Table.Cell className="whitespace-normal text-sm text-kumo-subtle">{item.effect}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
              </div>
            ) : null}
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
