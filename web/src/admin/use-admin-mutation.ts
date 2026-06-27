import { useMutation, useQueryClient } from "@tanstack/react-query";

import type { AdminRequest } from "@/publish/publish-status";

/**
 * Shared admin mutation. On success it invalidates every `["admin", ...]` query.
 * The publish-status config-changes poll is an active `["admin", ...]` query, so
 * the global publish bar re-evaluates automatically — it lights up iff the change
 * actually altered what the server would render. Individual mutations therefore
 * never hard-code "is this dirty"; they just run and let the closure react.
 */
export function useAdminMutation<TVars, TData = unknown>(
  request: AdminRequest,
  mutationFn: (request: AdminRequest, vars: TVars) => Promise<TData>,
  options?: { onSuccess?: (data: TData, vars: TVars) => void }
) {
  const queryClient = useQueryClient();
  return useMutation<TData, Error, TVars>({
    mutationFn: (vars) => mutationFn(request, vars),
    onSuccess: (data, vars) => {
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
      options?.onSuccess?.(data, vars);
    }
  });
}
