import { describe, expect, it } from "vitest";

import type { UserConnectionInfo } from "../types";
import { proxyDetails, proxySummary } from "./user-dialogs";

type ConnectionProxy = UserConnectionInfo["nodes"][number]["proxies"][number];

function proxy(overrides: Partial<ConnectionProxy> = {}): ConnectionProxy {
  return {
    name: "tokyo",
    proxy_name: "tokyo-reality",
    host_tag: "v4",
    type: "vless_reality",
    server: "203.0.113.10",
    server_port: 443,
    uuid: "",
    flow: "",
    server_name: "",
    public_key: "",
    short_id: "",
    ...overrides
  };
}

describe("proxyDetails", () => {
  it("emits Reality credentials for VLESS", () => {
    const details = JSON.parse(
      proxyDetails(
        "edge-1",
        proxy({
          uuid: "11111111-2222-3333-4444-555555555555",
          flow: "xtls-rprx-vision",
          server_name: "www.amazon.com",
          public_key: "pubkey",
          short_id: "0123"
        })
      )
    );
    expect(details).toMatchObject({
      node: "edge-1",
      type: "vless_reality",
      uuid: "11111111-2222-3333-4444-555555555555",
      flow: "xtls-rprx-vision",
      reality_public_key: "pubkey",
      reality_short_id: "0123"
    });
    expect(details).not.toHaveProperty("cipher");
    expect(details).not.toHaveProperty("password");
  });

  it("emits cipher and password for Shadowsocks 2022", () => {
    const details = JSON.parse(
      proxyDetails(
        "edge-1",
        proxy({
          type: "shadowsocks_2022",
          cipher: "2022-blake3-aes-128-gcm",
          password: "serverkey:userkey"
        })
      )
    );
    expect(details).toMatchObject({
      type: "shadowsocks_2022",
      cipher: "2022-blake3-aes-128-gcm",
      password: "serverkey:userkey"
    });
    expect(details).not.toHaveProperty("uuid");
    expect(details).not.toHaveProperty("flow");
    expect(details).not.toHaveProperty("reality_public_key");
  });

  it("includes the dialer Path when the Path is chained", () => {
    const details = JSON.parse(
      proxyDetails("edge-1", proxy({ type: "shadowsocks_2022", dialer_proxy: "relay" }))
    );
    expect(details.dialer_proxy).toBe("relay");
  });
});

describe("proxySummary", () => {
  it("shows the flow for VLESS", () => {
    expect(proxySummary(proxy({ flow: "xtls-rprx-vision" }))).toBe(
      "203.0.113.10:443 · vless_reality · xtls-rprx-vision"
    );
  });

  it("shows the cipher instead of an empty flow for Shadowsocks 2022", () => {
    expect(proxySummary(proxy({ type: "shadowsocks_2022", cipher: "2022-blake3-aes-256-gcm" }))).toBe(
      "203.0.113.10:443 · shadowsocks_2022 · 2022-blake3-aes-256-gcm"
    );
  });

  it("omits an empty credential segment", () => {
    expect(proxySummary(proxy())).toBe("203.0.113.10:443 · vless_reality");
  });
});
