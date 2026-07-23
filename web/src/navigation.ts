import {
  BroadcastIcon,
  BracketsCurlyIcon,
  FileTextIcon,
  GaugeIcon,
  GearSixIcon,
  HardDrivesIcon,
  PathIcon,
  PulseIcon,
  UsersIcon
} from "@phosphor-icons/react";
import type { Icon } from "@phosphor-icons/react";

import type { Page } from "./types";

export type NavItem = { id: Page; label: string; icon: Icon; path: string };

export const pages: NavItem[] = [
  { id: "overview", label: "Overview", icon: PulseIcon, path: "/" },
  { id: "nodes", label: "Nodes", icon: HardDrivesIcon, path: "/nodes" },
  { id: "proxies", label: "Proxies", icon: PathIcon, path: "/proxies" },
  { id: "users", label: "Users", icon: UsersIcon, path: "/users" },
  { id: "mihomo-profiles", label: "Mihomo Profiles", icon: BracketsCurlyIcon, path: "/mihomo-profiles" },
  { id: "traffic", label: "Traffic", icon: GaugeIcon, path: "/traffic" },
  { id: "network-events", label: "Network Events", icon: BroadcastIcon, path: "/network-events" },
  { id: "system-logs", label: "System Logs", icon: FileTextIcon, path: "/system-logs" },
  { id: "settings", label: "Settings", icon: GearSixIcon, path: "/settings" }
];

const byId = (id: Page): NavItem => pages.find((page) => page.id === id)!;

// Verb-based groups, Cloudflare-style. Overview is ungrouped at the top;
// Settings is rendered separately at the bottom under a divider.
export const navGroups: Array<{ label?: string; items: NavItem[] }> = [
  { items: [byId("overview")] },
  { label: "Operate", items: [byId("nodes"), byId("proxies")] },
  { label: "Manage", items: [byId("users"), byId("mihomo-profiles"), byId("traffic")] },
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
