import { Activity, FileText, Gauge, RadioTower, Router, Server, Users } from "lucide-react";
import type { LucideIcon } from "lucide-react";

import type { Page } from "./types";

export const pages: Array<{ id: Page; label: string; icon: LucideIcon; path: string }> = [
  { id: "overview", label: "Overview", icon: Activity, path: "/" },
  { id: "nodes", label: "Nodes", icon: Server, path: "/nodes" },
  { id: "proxies", label: "Proxies", icon: Router, path: "/proxies" },
  { id: "users", label: "Users", icon: Users, path: "/users" },
  { id: "traffic", label: "Traffic", icon: Gauge, path: "/traffic" },
  { id: "network-events", label: "Network Events", icon: RadioTower, path: "/network-events" },
  { id: "system-logs", label: "System Logs", icon: FileText, path: "/system-logs" }
];

export function adminBasename(pathname = window.location.pathname): string {
  const adminIndex = pathname.indexOf("/admin");
  if (adminIndex < 0) {
    return "/admin";
  }
  return pathname.slice(0, adminIndex + "/admin".length) || "/admin";
}
