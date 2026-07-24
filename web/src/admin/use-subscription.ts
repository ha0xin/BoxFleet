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
  const generate = useAdminMutation<void, Subscription>(request, (req) =>
    req(endpoint, { method: "POST" })
  );
  const rotate = useAdminMutation<void, Subscription>(
    request,
    (req) => req(`${endpoint}/rotate`, { method: "POST" }),
    { onSuccess: onDestructiveSuccess }
  );
  const revoke = useAdminMutation<void, Subscription>(
    request,
    (req) => req(endpoint, { method: "DELETE" }),
    { onSuccess: onDestructiveSuccess }
  );
  return { query, generate, rotate, revoke };
}
