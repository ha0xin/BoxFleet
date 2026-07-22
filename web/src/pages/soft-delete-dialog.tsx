import type { ReactNode } from "react";
import { Banner, Button, Dialog } from "@cloudflare/kumo";

import type { AdminRequest } from "@/publish/publish-status";
import { useAdminMutation } from "@/admin/use-admin-mutation";

export function SoftDeleteDialog({
  request,
  title,
  description,
  endpoint,
  onClose
}: {
  request: AdminRequest;
  title: string;
  description: ReactNode;
  endpoint: string;
  onClose: () => void;
}) {
  const mutation = useAdminMutation<void, unknown>(
    request,
    (req) => req(endpoint, { method: "DELETE" }),
    { onSuccess: onClose }
  );

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog size="sm" className="p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">{title}</Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">{description}</Dialog.Description>
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
