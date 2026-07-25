import { describe, expect, it } from "vitest";

import type { AdminNode } from "../types";
import { formatNodeVersion } from "./operations-common";

function node(overrides: Partial<AdminNode> = {}): AdminNode {
  return {
    id: "node-1",
    name: "edge-1",
    public_host: "edge-1.example.com",
    api_base_url: "",
    status: "pending",
    sing_box_version: "sing-box version 1.13.13",
    last_seen_at: "",
    deleted_at: "",
    ...overrides
  };
}

describe("formatNodeVersion", () => {
  it("does not use the sing-box version as the current config version", () => {
    expect(formatNodeVersion(node({ target_version: "1" }))).toBe("n/a -> 1");
  });

  it("formats applied and pending config versions", () => {
    expect(formatNodeVersion(node({ current_version: "1", target_version: "1" }))).toBe("1");
    expect(formatNodeVersion(node({ current_version: "1", target_version: "2" }))).toBe("1 -> 2");
  });

  it("shows n/a when no config version exists", () => {
    expect(formatNodeVersion(node())).toBe("n/a");
  });
});
