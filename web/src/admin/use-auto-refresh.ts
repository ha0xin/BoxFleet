import { useEffect, useRef } from "react";

/**
 * Runs `refresh` every `intervalMs` while `enabled` and the tab is visible.
 * Pages whose query keys embed a frozen "now" anchor (Traffic, Network Events)
 * use this to replay their manual Refresh button on a cadence — a plain
 * `refetchInterval` would re-request the same frozen window forever.
 */
export function useAutoRefresh(intervalMs: number, enabled: boolean, refresh: () => void) {
  const refreshRef = useRef(refresh);
  useEffect(() => {
    refreshRef.current = refresh;
  });
  useEffect(() => {
    if (!enabled) return;
    const timer = setInterval(() => {
      if (document.hidden) return;
      refreshRef.current();
    }, intervalMs);
    return () => clearInterval(timer);
  }, [enabled, intervalMs]);
}
