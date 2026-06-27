import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import type { AdminNode, ConfigChange, ConfigChangesResponse, PublishResponse } from "@/types";

export type AdminRequest = <T>(path: string, init?: RequestInit) => Promise<T>;

/**
 * Visible state of the global publish bar.
 *
 * - `idle`       — config on every node matches what the server would render.
 * - `dirty`      — there are unpublished changes; bar turns blue, offers Apply.
 * - `publishing` — POST /config/publish in flight.
 * - `applying`   — published; waiting for agents to pull + apply (polls nodes).
 * - `applied`    — every node reached the target; bar turns green + sheen.
 * - `failed`     — publish failed, or a node reported apply failure.
 */
export type PublishStatus = "idle" | "dirty" | "publishing" | "applying" | "applied" | "failed";

export type ApplyProgress = { applied: number; failed: number; total: number };

type PublishContextValue = {
  status: PublishStatus;
  changes: ConfigChange[];
  changedCount: number;
  /** Message when GET /config/changes is failing, else null. */
  changesError: string | null;
  progress: ApplyProgress;
  /** Open the diff review dialog. */
  openDiff: () => void;
  /** Close the diff review dialog. */
  closeDiff: () => void;
  isDiffOpen: boolean;
  /** Confirm + publish all changed nodes (closes the dialog). */
  publish: () => void;
  /** Dismiss a terminal `applied`/`failed` banner back to idle. */
  dismiss: () => void;
};

const PublishContext = createContext<PublishContextValue | null>(null);

// Imperative phases drive the publish→apply animation; the resting phase
// (`browsing`) defers to whether there are unpublished changes.
type Phase = "browsing" | "publishing" | "applying" | "celebrating" | "error";

const CELEBRATE_MS = 2600;

function nodeIsTracked(node: AdminNode): boolean {
  // Disabled nodes never apply config; pending nodes have not enrolled yet.
  return node.status !== "disabled" && node.status !== "pending";
}

function nodeApplied(node: AdminNode): boolean {
  if (node.apply_status && node.apply_status !== "applied") return false;
  if (node.target_version && node.current_version) {
    return node.current_version === node.target_version;
  }
  return node.apply_status === "applied";
}

export function PublishStatusProvider({ request, children }: { request: AdminRequest; children: ReactNode }) {
  const queryClient = useQueryClient();
  const [phase, setPhase] = useState<Phase>("browsing");
  const [isDiffOpen, setDiffOpen] = useState(false);
  // Node names this operation actually published. Convergence is judged only
  // over these, so an unrelated node stuck in a prior failed/pending state can
  // neither falsely fail nor indefinitely block this publish.
  const publishedNodesRef = useRef<Set<string>>(new Set());

  const changesQuery = useQuery({
    queryKey: ["admin", "config-changes"],
    queryFn: () => request<ConfigChangesResponse>("/api/admin/config/changes"),
    refetchInterval: 15_000
  });
  const changes = useMemo(() => changesQuery.data?.changed ?? [], [changesQuery.data]);
  // Surface a failing /config/changes instead of silently reading as idle: a
  // server that cannot render/compare configs must not look "up to date".
  const changesError = changesQuery.isError
    ? changesQuery.error instanceof Error
      ? changesQuery.error.message
      : "Unable to load config changes"
    : null;

  // Poll node state quickly while waiting for agents to apply the new config, and
  // keep polling after a failure so an agent that retries and converges is seen.
  const polling = phase === "applying" || phase === "error";
  const nodesQuery = useQuery({
    queryKey: ["admin", "publish-nodes"],
    queryFn: () => request<AdminNode[]>("/api/admin/nodes"),
    enabled: polling,
    refetchInterval: polling ? 4_000 : false
  });

  const progress = useMemo<ApplyProgress>(() => {
    const published = publishedNodesRef.current;
    const tracked = (nodesQuery.data ?? []).filter(
      (node) => published.has(node.name) && nodeIsTracked(node)
    );
    return {
      total: tracked.length,
      applied: tracked.filter(nodeApplied).length,
      failed: tracked.filter((n) => n.apply_status === "failed").length
    };
  }, [nodesQuery.data]);

  // Drive the apply phase off live node state. Convergence (every published node
  // applied, none failed) wins even from `error`, so an agent that recovers on a
  // later retry flips the bar green instead of staying red until dismissed.
  // total === 0 (nothing left to wait on) counts as converged so we never hang.
  useEffect(() => {
    if ((phase !== "applying" && phase !== "error") || !nodesQuery.data) return;
    if (progress.failed === 0 && progress.applied >= progress.total) {
      setPhase("celebrating");
      return;
    }
    if (progress.failed > 0) {
      setPhase("error");
    }
  }, [phase, nodesQuery.data, progress]);

  // Auto-clear the green celebration back to idle after the sheen plays.
  useEffect(() => {
    if (phase !== "celebrating") return;
    const timer = setTimeout(() => setPhase("browsing"), CELEBRATE_MS);
    return () => clearTimeout(timer);
  }, [phase]);

  const publishMutation = useMutation({
    mutationFn: () => request<PublishResponse>("/api/admin/config/publish", { method: "POST" }),
    onMutate: () => setPhase("publishing"),
    onSuccess: (data) => {
      publishedNodesRef.current = new Set(data.published.map((result) => result.node));
      // Drop the stale node snapshot from the previous apply: its versions match
      // and would otherwise trip an immediate false "applied" before the first
      // fresh poll carrying the just-published target arrives.
      queryClient.removeQueries({ queryKey: ["admin", "publish-nodes"] });
      setPhase("applying");
      void changesQuery.refetch();
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: () => setPhase("error")
  });

  // Keep a stable ref so the callbacks below don't re-create every render.
  const mutateRef = useRef(publishMutation.mutate);
  mutateRef.current = publishMutation.mutate;

  const openDiff = useCallback(() => setDiffOpen(true), []);
  const closeDiff = useCallback(() => setDiffOpen(false), []);
  const publish = useCallback(() => {
    setDiffOpen(false);
    mutateRef.current();
  }, []);
  const dismiss = useCallback(() => setPhase("browsing"), []);

  const status: PublishStatus = useMemo(() => {
    switch (phase) {
      case "publishing":
        return "publishing";
      case "applying":
        return "applying";
      case "celebrating":
        return "applied";
      case "error":
        return "failed";
      default:
        return changes.length > 0 ? "dirty" : "idle";
    }
  }, [phase, changes.length]);

  const value = useMemo<PublishContextValue>(
    () => ({
      status,
      changes,
      changedCount: changes.length,
      changesError,
      progress,
      openDiff,
      closeDiff,
      isDiffOpen,
      publish,
      dismiss
    }),
    [status, changes, changesError, progress, openDiff, closeDiff, isDiffOpen, publish, dismiss]
  );

  return <PublishContext.Provider value={value}>{children}</PublishContext.Provider>;
}

export function usePublishStatus(): PublishContextValue {
  const ctx = useContext(PublishContext);
  if (!ctx) {
    throw new Error("usePublishStatus must be used within a PublishStatusProvider");
  }
  return ctx;
}
