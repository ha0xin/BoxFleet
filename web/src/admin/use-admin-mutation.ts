import { useMutation, useQueryClient } from "@tanstack/react-query";

import type { AdminRequest } from "@/admin/api";
import { adminKeys } from "@/admin/query";

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
  options?: { onSuccess?: (data: TData, vars: TVars) => void | Promise<void> }
) {
  const queryClient = useQueryClient();
  return useMutation<TData, Error, TVars>({
    mutationFn: (vars) => mutationFn(request, vars),
    onSuccess: async (data, vars) => {
      await queryClient.invalidateQueries({ queryKey: adminKeys.root });
      await options?.onSuccess?.(data, vars);
    }
  });
}
