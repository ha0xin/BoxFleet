import { useMutation, useQueryClient } from "@tanstack/react-query";

import type { AdminRequest } from "@/admin/api";
import { adminKeys } from "@/admin/query";
import { toastError } from "@/admin/toast";

/**
 * Shared admin mutation. On success it invalidates every `["admin", ...]` query.
 * The publish-status config-changes poll is an active `["admin", ...]` query, so
 * the global publish bar re-evaluates automatically — it lights up iff the change
 * actually altered what the server would render. Individual mutations therefore
 * never hard-code "is this dirty"; they just run and let the closure react.
 *
 * Errors surface as an error toast by default so actions fired from menus and
 * inline buttons never fail silently. Dialogs that already render an inline
 * `<Banner>` for `mutation.error` pass `toastError: false` to avoid doubling up.
 */
export function useAdminMutation<TVars, TData = unknown>(
  request: AdminRequest,
  mutationFn: (request: AdminRequest, vars: TVars) => Promise<TData>,
  options?: { onSuccess?: (data: TData, vars: TVars) => void | Promise<void>; toastError?: boolean }
) {
  const queryClient = useQueryClient();
  return useMutation<TData, Error, TVars>({
    mutationFn: (vars) => mutationFn(request, vars),
    onSuccess: async (data, vars) => {
      await queryClient.invalidateQueries({ queryKey: adminKeys.root });
      await options?.onSuccess?.(data, vars);
    },
    onError: (error) => {
      if (options?.toastError !== false) {
        toastError("Request failed", error.message);
      }
    }
  });
}
