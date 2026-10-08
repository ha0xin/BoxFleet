import { expect, test } from "@playwright/test";

for (const width of [390, 651, 1440]) {
  test(`unified Logs displays both sources and redirects old stream links at ${width}px`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 824 });
    const at = new Date().toISOString();
    const base = { node_name: "edge", user_name: "alice", auth_name: "vless@alice", source_ip: "192.168.100.123", target_host: "example.com", target_port: 443, action: "connect", raw_message: "", count: 1, window_start: at, window_end: at, created_at: at };
    await page.route("**/api/admin/network-events?*", route => route.fulfill({ json: { total: 2, events: [
      { ...base, id: "stream-1", source: "stream", connection_id: "session-1", network: "tcp", uplink_bytes: 1024, downlink_bytes: 2048, duration_ms: 60000, connections_closed: 1, outbound: "direct" },
      { ...base, id: "journal-1", source: "journal", source_ip: "", raw_message: "legacy context", network: null, uplink_bytes: null, downlink_bytes: null, duration_ms: null, connections_closed: null }
    ] } }));
    await page.goto("network-events?view=connections");
    await expect(page).not.toHaveURL(/view=connections/);
    await expect(page.getByRole("tab", { name: "Logs", exact: true })).toBeVisible();
    await expect(page.getByRole("tab", { name: "Connection stream" })).toHaveCount(0);
    const table = page.locator("table").last();
    await expect(table.locator("tbody > tr")).toHaveCount(2);
    const raw = table.locator("tbody > tr").first();
    const old = table.locator("tbody > tr").nth(1);
    await expect(raw).toContainText("192.168.100.123");
    await expect(raw).toContainText("tcp");
    await expect(raw).toContainText("1.0 KB");
    await expect(raw).toContainText("1m");
    await expect(old.locator("td").nth(4)).toHaveText("—");
    await expect(old.locator("td").nth(9)).toHaveText("—");
    await raw.getByRole("button", { name: "Expand network event" }).click();
    await expect(page.getByLabel("Log entry details")).toContainText("session-1");
    const geometry = await page.evaluate(() => ({ viewport: window.innerWidth, page: document.documentElement.scrollWidth }));
    expect(geometry.page).toBeLessThanOrEqual(geometry.viewport);
    await page.screenshot({ path: testInfo.outputPath(`unified-logs-${width}.png`) });
  });
}
