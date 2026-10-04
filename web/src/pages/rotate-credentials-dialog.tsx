import { Banner, Button, Dialog } from "@cloudflare/kumo";
import type { AdminRequest } from "@/admin/api";
import { useAdminMutation } from "@/admin/use-admin-mutation";
import type { AdminUser } from "../types";

export function RotateCredentialsDialog({ request, user, onClose }: {
  request: AdminRequest;
  user: AdminUser;
  onClose: () => void;
}) {
  const mutation = useAdminMutation<void, { rotated: number }>(request,
    (req) => req(`/api/admin/users/${encodeURIComponent(user.name)}/credentials/rotate`, { method: "POST" }),
    { toastError: false });
  return (
    <Dialog.Root open onOpenChange={(open) => { if (!open && !mutation.isPending) onClose(); }}>
      <Dialog size="base" className="max-h-[calc(100dvh-2rem)] overflow-y-auto p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">Rotate connection keys</Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          Replace all VLESS UUIDs and Shadowsocks passwords for {user.name}, including disabled credentials.
          Access, traffic usage, and subscription links stay the same.
          After Review &amp; apply, clients must refresh their subscription or import the new keys.
        </Dialog.Description>
        {mutation.isError ? <Banner variant="error" title={mutation.error.message} className="mb-4" /> : null}
        {mutation.isSuccess ? (
          <Banner variant="default" title={mutation.data.rotated > 0 ? `${mutation.data.rotated} connection ${mutation.data.rotated === 1 ? "key" : "keys"} replaced` : "No connection keys to replace"} className="mb-4">
            {mutation.data.rotated > 0 ? "Use Review & apply to activate the new keys on nodes. Old keys remain valid until the new configuration is applied." : null}
          </Banner>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button variant="ghost" disabled={mutation.isPending} onClick={onClose}>{mutation.isSuccess ? "Done" : "Cancel"}</Button>
          {!mutation.isSuccess ? <Button variant="destructive" loading={mutation.isPending} onClick={() => mutation.mutate()}>Rotate all keys</Button> : null}
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
