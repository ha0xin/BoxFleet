import { useQuery } from "@tanstack/react-query";

import type { AdminRequest } from "./api";
import { useAdminMutation } from "./use-admin-mutation";

export type SubscriptionRecord = {
  active: boolean;
  url: string;
  created_at: string;
  last_used_at: string;
};

export function useSubscription<Subscription extends SubscriptionRecord>(
  request: AdminRequest,
  queryKey: readonly unknown[],
  endpoint: string,
  onDestructiveSuccess?: () => void
) {
  const query = useQuery({ queryKey, queryFn: () => request<Subscription>(endpoint) });
  // Consumers render these mutation errors in an inline Banner, so skip the
  // global error toast.
  const generate = useAdminMutation<void, Subscription>(
    request,
    (req) => req(endpoint, { method: "POST" }),
    { toastError: false }
  );
  const rotate = useAdminMutation<void, Subscription>(
    request,
    (req) => req(`${endpoint}/rotate`, { method: "POST" }),
    { onSuccess: onDestructiveSuccess, toastError: false }
  );
  const revoke = useAdminMutation<void, Subscription>(
    request,
    (req) => req(endpoint, { method: "DELETE" }),
    { onSuccess: onDestructiveSuccess, toastError: false }
  );
  return { query, generate, rotate, revoke };
}
