import { expect, test } from "@playwright/test";

const server = `http://127.0.0.1:${process.env.BOXFLEET_E2E_SERVER_PORT ?? "18081"}`;

test("node addresses show multiple local and observed IPs, and retain inventory on failure", async ({ page, request }) => {
  const name = "address-discovery-e2e";
  const enrolled = await request.post(`${server}/api/admin/nodes/bootstrap`, {
    data: { name, public_host: "nat-entry.example" }
  });
  expect(enrolled.ok()).toBeTruthy();
  const { node, bootstrap_string } = await enrolled.json();
  const { token } = JSON.parse(Buffer.from(bootstrap_string.slice("boxfleet-bootstrap:".length), "base64url").toString());
  const headers = { Authorization: `Bearer ${token}`, "X-BoxFleet-Node": name };
  const addresses = ["10.0.0.2", "10.0.0.3", "2001:db8:1234:5678::2", "2001:db8:1234:5678::3"].map((address) => ({
    address, family: address.includes(":") ? 6 : 4, prefix_length: address.includes(":") ? 64 : 24,
    interface: "eth0", interface_index: 2, interface_up: true, scope: "global", flags_known: true
  }));
  const snapshot = { revision: 1, complete: true, collected_at: new Date().toISOString(), addresses };
  expect((await request.post(`${server}/api/node/network-snapshot`, { headers, data: snapshot })).ok()).toBeTruthy();
  for (const ip of ["198.51.100.9", "198.51.100.10", "2001:db8::9", "2001:db8::10"]) {
    const heartbeat = await request.post(`${server}/api/node/heartbeat`, {
      headers: { ...headers, "CF-Connecting-IP": ip },
      data: { status: "ok", capabilities: ["network.inventory.v1"] }
    });
    expect(heartbeat.ok()).toBeTruthy();
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("nodes");
  await page.getByRole("button", { name: `View addresses for ${name}`, exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("heading", { name: `Addresses · ${name}` })).toBeVisible();
  for (const address of addresses) await expect(dialog.getByText(`${address.address}/${address.prefix_length}`, { exact: true })).toBeVisible();
  for (const ip of ["198.51.100.9", "198.51.100.10", "2001:db8::9", "2001:db8::10"]) await expect(dialog.getByText(ip, { exact: true })).toBeVisible();
  await expect(dialog.getByText("nat-entry.example", { exact: true })).toBeVisible();

  const geometry = await dialog.evaluate((element) => ({
    width: element.getBoundingClientRect().width,
    scrollHeight: element.scrollHeight,
    clientHeight: element.clientHeight,
    overflowY: getComputedStyle(element).overflowY
  }));
  expect(geometry.width).toBe(768);
  expect(geometry.overflowY).toBe("auto");
  await page.screenshot({ path: "test-results/node-addresses-desktop.png" });

  expect((await request.post(`${server}/api/node/network-snapshot`, {
    headers, data: { revision: 2, complete: false, collected_at: new Date().toISOString(), error: "dump interrupted", addresses: [] }
  })).ok()).toBeTruthy();
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  await page.getByRole("button", { name: `View addresses for ${name}`, exact: true }).click();
  await expect(dialog.getByText(/Collection failed: dump interrupted/)).toBeVisible();
  await expect(dialog.getByText("10.0.0.2/24", { exact: true })).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  const bounds = await dialog.evaluate((element) => {
    const r = element.getBoundingClientRect();
    return { left: r.left, right: r.right, width: window.innerWidth };
  });
  expect(bounds.left).toBeGreaterThanOrEqual(0);
  expect(bounds.right).toBeLessThanOrEqual(bounds.width);
  const tableGeometry = await dialog.getByRole("table").evaluate((table) => ({
    width: table.getBoundingClientRect().width,
    overflow: getComputedStyle(table.parentElement!).overflowX
  }));
  expect(tableGeometry.width).toBeGreaterThanOrEqual(640);
  expect(tableGeometry.overflow).toBe("auto");
  await page.screenshot({ path: "test-results/node-addresses-mobile.png" });
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  expect((await request.delete(`${server}/api/admin/nodes/${node.id}`)).ok()).toBeTruthy();
});

test("older agents still expose public observations without claiming local inventory", async ({ page, request }) => {
  const name = "address-legacy-e2e";
  const enrolled = await request.post(`${server}/api/admin/nodes/bootstrap`, { data: { name, public_host: "legacy.example" } });
  expect(enrolled.ok()).toBeTruthy();
  const { node, bootstrap_string } = await enrolled.json();
  const { token } = JSON.parse(Buffer.from(bootstrap_string.slice("boxfleet-bootstrap:".length), "base64url").toString());
  expect((await request.post(`${server}/api/node/heartbeat`, {
    headers: { Authorization: `Bearer ${token}`, "X-BoxFleet-Node": name, "CF-Connecting-IP": "198.51.100.20" },
    data: { status: "ok" }
  })).ok()).toBeTruthy();
  await page.goto("nodes");
  await page.getByRole("button", { name: `View addresses for ${name}`, exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("198.51.100.20", { exact: true })).toBeVisible();
  await expect(dialog.getByText("Upgrade the agent to discover local addresses.", { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  expect((await request.delete(`${server}/api/admin/nodes/${node.id}`)).ok()).toBeTruthy();
});
