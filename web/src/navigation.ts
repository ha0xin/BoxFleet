import { Broadcast, FileText, GearSix, Gauge, HardDrives, Path, Pulse, Users } from "@phosphor-icons/react";
import type { Icon } from "@phosphor-icons/react";

import type { Page } from "./types";

export type NavItem = { id: Page; label: string; icon: Icon; path: string };

export const pages: NavItem[] = [
  { id: "overview", label: "Overview", icon: Pulse, path: "/" },
  { id: "nodes", label: "Nodes", icon: HardDrives, path: "/nodes" },
  { id: "proxies", label: "Proxies", icon: Path, path: "/proxies" },
  { id: "users", label: "Users", icon: Users, path: "/users" },
  { id: "traffic", label: "Traffic", icon: Gauge, path: "/traffic" },
  { id: "network-events", label: "Network Events", icon: Broadcast, path: "/network-events" },
  { id: "system-logs", label: "System Logs", icon: FileText, path: "/system-logs" },
  { id: "settings", label: "Settings", icon: GearSix, path: "/settings" }
];

const byId = (id: Page): NavItem => pages.find((page) => page.id === id)!;

// Verb-based groups, Cloudflare-style. Overview is ungrouped at the top;
// Settings is rendered separately at the bottom under a divider.
export const navGroups: Array<{ label?: string; items: NavItem[] }> = [
  { items: [byId("overview")] },
  { label: "Operate", items: [byId("nodes"), byId("proxies")] },
  { label: "Manage", items: [byId("users"), byId("traffic")] },
  { label: "Observe", items: [byId("network-events"), byId("system-logs")] }
];

export const settingsNav = byId("settings");

export function adminBasename(pathname = window.location.pathname): string {
  const adminIndex = pathname.indexOf("/admin");
  if (adminIndex < 0) {
    return "/admin";
  }
  return pathname.slice(0, adminIndex + "/admin".length) || "/admin";
}
