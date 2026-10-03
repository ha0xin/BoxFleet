import { expect, test } from "@playwright/test";

test("mobile navigation and wide tables remain reachable", async ({ page }) => {
  const consoleErrors: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error") consoleErrors.push(message.text());
  });

  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("nodes");
  await expect(page.getByRole("heading", { name: "Nodes", exact: true })).toBeVisible();

  const mobileSidebarTrigger = page.locator('main button[aria-label*="sidebar"]');
  await expect(mobileSidebarTrigger).toBeVisible();
  await mobileSidebarTrigger.click();
  await expect(page.getByRole("button", { name: "Nodes", exact: true })).toBeVisible();

  await page.setViewportSize({ width: 1180, height: 900 });
  // Page-size controls are hidden while the table is empty, so drive the
  // URL-synced limit directly and assert the empty pagination state.
  await page.goto("network-events?limit=50");
  await expect(page.getByRole("button", { name: "Add filter", exact: true })).toBeVisible();
  await expect(page.getByText("0 items", { exact: true })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Page size", exact: true })).toHaveCount(0);
  const tableGeometry = await page.locator(".bf-table-scroll").evaluate((element) => ({
    clientWidth: element.clientWidth,
    scrollWidth: element.scrollWidth,
    pageWidth: document.documentElement.scrollWidth,
    viewportWidth: window.innerWidth
  }));
  expect(tableGeometry.scrollWidth).toBeGreaterThan(tableGeometry.clientWidth);
  expect(tableGeometry.pageWidth).toBe(tableGeometry.viewportWidth);
  expect(consoleErrors.filter((message) =>
    message.includes("Query data cannot be undefined") || message.includes("width(-1)") || message.includes("height(-1)")
  )).toEqual([]);
});

test("column widths resize, survive reload, hide and reset independently", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("proxies");
  const proxyHead = page.getByRole("columnheader", { name: "Proxy", exact: true });
  const nodeHead = page.getByRole("columnheader", { name: "Node", exact: true });
  const original = (await proxyHead.boundingBox())!.width;
  const nodeWidth = (await nodeHead.boundingBox())!.width;
  const handle = page.getByRole("separator", { name: "Resize Proxy column", exact: true });
  const bounds = (await handle.boundingBox())!;
  await page.mouse.move(bounds.x + bounds.width / 2, bounds.y + bounds.height / 2);
  await page.mouse.down();
  await page.mouse.move(bounds.x + bounds.width / 2 + 60, bounds.y + bounds.height / 2, { steps: 8 });
  await page.mouse.up();
  await expect.poll(async () => (await proxyHead.boundingBox())!.width).toBeCloseTo(original + 60, 0);
  expect((await nodeHead.boundingBox())!.width).toBeCloseTo(nodeWidth, 0);
  await page.reload();
  await expect.poll(async () => (await proxyHead.boundingBox())!.width).toBeCloseTo(original + 60, 0);
  await handle.focus();
  await handle.press("ArrowRight");
  await expect.poll(async () => (await proxyHead.boundingBox())!.width).toBeCloseTo(original + 70, 0);
  await page.getByRole("button", { name: "Edit columns", exact: true }).click();
  await page.getByRole("menuitemcheckbox", { name: "Node", exact: true }).click();
  await page.keyboard.press("Escape");
  await expect(nodeHead).toHaveCount(0);
  await page.reload();
  await expect(nodeHead).toHaveCount(0);
  await page.getByRole("button", { name: "Edit columns", exact: true }).click();
  await page.getByRole("menuitem", { name: "Reset columns", exact: true }).click();
  await expect(nodeHead).toBeVisible();
  await expect.poll(async () => (await proxyHead.boundingBox())!.width).toBeCloseTo(original, 0);
});

for (const width of [390, 768, 1024, 1440, 1920]) {
  test(`Cloudflare table chrome contains overflow at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    for (const route of ["nodes", "proxies", "paths", "users", "mihomo-profiles", "system-logs", "network-events"]) {
      await page.goto(route);
      await expect(page.locator(".bf-table-card").first()).toBeVisible();
      const geometry = await page.locator(".bf-table-card").first().evaluate((element) => ({
        viewport: innerWidth,
        page: document.documentElement.scrollWidth,
        card: element.getBoundingClientRect().width,
        headerHeight: element.querySelector("th")?.getBoundingClientRect().height,
        fade: element.querySelector("th") ? getComputedStyle(element.querySelector("th")!, "::before").display : "none"
      }));
      expect(geometry.page, `${route} at ${width}px`).toBe(geometry.viewport);
      expect(geometry.card).toBeLessThanOrEqual(width);
      expect(geometry.headerHeight).toBeCloseTo(44, 1);
      expect(geometry.fade).toBe("none");
    }
  });
}


test("log fields and inline details stay aligned after hiding a column", async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.route("**/api/admin/system-logs?*", (route) => route.fulfill({ json: {
    total: 1, services: ["sing-box"], logs: [{ observed_at: "2026-10-03T01:02:03.123Z", ingested_at: "2026-10-03T01:02:04Z", node: "log-test", service: "sing-box", level: "info", message: "full journal detail" }]
  } }));
  await page.goto("system-logs");
  await page.getByRole("button", { name: "Expand log entry", exact: true }).click();
  await expect(page.getByLabel("Log entry details")).toContainText("full journal detail");
  await page.screenshot({ path: testInfo.outputPath("logs-desktop.png") });
  await page.getByRole("button", { name: "Fields", exact: true }).click();
  await page.getByRole("menuitemcheckbox", { name: "Message", exact: true }).click();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("columnheader", { name: "Message", exact: true })).toHaveCount(0);
  const columns = await page.locator(".bf-data-table thead th").count();
  expect(await page.locator(".bf-log-detail td").getAttribute("colspan")).toBe(String(columns));
  await page.getByRole("button", { name: "Collapse log entry", exact: true }).click();
  await expect(page.getByLabel("Log entry details")).toHaveCount(0);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: testInfo.outputPath("logs-mobile.png") });
});

test("resource rows use Domains density with full protocol labels", async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.route("**/api/admin/proxies?*", (route) => route.fulfill({ json: { total: 2, proxies: [
    { id: "preview-reality", name: "SG-Reality", node_name: "Singapore", enabled: true, protocol: "vless_reality", listen: "::", listen_port: 443, transport: "tcp", traffic_multiplier: 1, updated_at: "2026-10-03T01:02:03Z" },
    { id: "preview-ss", name: "US-Shadowsocks", node_name: "Los Angeles", enabled: false, protocol: "shadowsocks_2022", listen: "::", listen_port: 8080, transport: "tcp_udp", traffic_multiplier: 1, updated_at: "2026-10-03T01:02:03Z" }
  ] } }));
  await page.goto("proxies");
  await expect(page.locator(".bf-data-table tbody tr")).toHaveCount(2);
  expect((await page.locator(".bf-data-table tbody tr").first().boundingBox())!.height).toBeCloseTo(44, 1);
  const headerColor = await page.locator(".bf-data-table th").first().evaluate((element) => getComputedStyle(element).color);
  const subtleColor = await page.locator("header p").evaluate((element) => getComputedStyle(element).color);
  expect(headerColor).toBe(subtleColor);
  const protocol = page.locator(".bf-data-table tbody tr").last().locator("td").nth(3);
  expect(await protocol.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
  await page.screenshot({ path: testInfo.outputPath("resources-desktop.png") });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: testInfo.outputPath("resources-mobile.png") });
});


test("resource sort indicators match Domains and do not recolor column controls", async ({ page }, testInfo) => {
  await page.goto("proxies?sort=name&direction=asc");
  const proxy = page.getByRole("columnheader", { name: "Proxy", exact: true });
  const node = page.getByRole("columnheader", { name: "Node", exact: true });
  await expect(node.locator(".bf-sort-icon")).toHaveAttribute("data-sort-indicator", "none");
  await node.getByRole("button", { name: "Node", exact: true }).click();
  await expect(node).toHaveAttribute("aria-sort", "ascending");
  await expect(proxy.locator(".bf-sort-icon")).toHaveAttribute("data-sort-indicator", "none");
  const ascending = await node.locator(".bf-sort-icon").evaluate((icon) => ({
    width: icon.getBoundingClientRect().width, opacity: getComputedStyle(icon).opacity,
    transform: getComputedStyle(icon).transform, path: icon.querySelector("path")!.getAttribute("d")
  }));
  expect(ascending.width).toBe(12);
  expect(ascending.opacity).toBe("0.5");
  await node.getByRole("button", { name: "Node", exact: true }).click();
  await expect(node).toHaveAttribute("aria-sort", "descending");
  await expect(node.locator(".bf-sort-icon")).toHaveAttribute("data-sort-indicator", "desc");
  await expect(node.locator(".bf-sort-icon")).toHaveCSS("transform", "none");
  expect(await node.locator(".bf-sort-icon path").getAttribute("d")).toBe(ascending.path);
  await expect(page.getByRole("separator", { name: "Resize Node column", exact: true })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("resource-sort.png") });
  await page.goto("system-logs");
  const timestamp = page.getByRole("columnheader", { name: "Timestamp", exact: true });
  await expect(timestamp).toHaveAttribute("aria-sort", "descending");
  const logIcon = await timestamp.locator(".bf-sort-icon").evaluate((icon) => ({ width: icon.getBoundingClientRect().width, opacity: getComputedStyle(icon).opacity }));
  expect(logIcon).toEqual({ width: 20, opacity: "1" });
  expect(await timestamp.getByRole("button", { name: "Fields", exact: true }).locator("svg").evaluate((icon) => getComputedStyle(icon).opacity)).toBe("1");
});

test("log workbench fields, column order and time query persist", async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const queries: URL[] = [];
  await page.route("**/api/admin/system-logs?*", (route) => {
    queries.push(new URL(route.request().url()));
    return route.fulfill({ json: { total: 1, services: ["sing-box"], logs: [{ observed_at: "2026-10-03T01:02:03.123Z", ingested_at: "2026-10-03T01:02:04Z", node: "workbench-node", service: "sing-box", level: "info", message: "workbench journal" }] } });
  });
  await page.goto("system-logs");
  await expect(page.getByText("workbench journal", { exact: true })).toBeVisible();
  const drag = page.getByRole("button", { name: "Drag Node column", exact: true });
  await drag.focus();
  await drag.press("Space");
  // dnd-kit measures droppable headers on animation frames after activation.
  await expect(page.getByRole("status")).toContainText("over droppable area 2");
  await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
  await drag.press("ArrowRight");
  await expect(page.getByRole("status")).toContainText("over droppable area 3");
  await drag.press("Space");
  const headLabels = () => page.locator(".bf-data-table thead th").evaluateAll((heads) => heads.map((head) => head.getAttribute("aria-label")));
  await expect.poll(headLabels).toEqual(["Details", "Timestamp", "Service", "Node", "Level", "Message", "Ingested", null]);
  const cells = page.locator(".bf-data-table tbody tr").first().locator("td");
  await expect(cells.nth(2)).toHaveText("sing-box");
  await expect(cells.nth(3)).toHaveText("workbench-node");
  await page.reload();
  await expect.poll(headLabels).toEqual(["Details", "Timestamp", "Service", "Node", "Level", "Message", "Ingested", null]);
  await page.getByRole("button", { name: "Toggle fields sidebar", exact: true }).click();
  const fields = page.getByRole("complementary", { name: "Log fields", exact: true });
  await fields.getByLabel("Search fields", { exact: true }).fill("Node");
  await fields.getByRole("checkbox", { name: "Node", exact: true }).uncheck();
  await expect(page.getByRole("columnheader", { name: "Node", exact: true })).toHaveCount(0);
  await fields.getByRole("button", { name: "Reset columns", exact: true }).click();
  await fields.getByRole("button", { name: "Close fields", exact: true }).click();
  await page.getByRole("combobox", { name: "Time range", exact: true }).click();
  await page.getByRole("option", { name: "Last 1 hour", exact: true }).click();
  await expect.poll(() => queries.at(-1)?.searchParams.has("start")).toBe(true);
  const request = queries.at(-1)!;
  expect(Date.parse(request.searchParams.get("end")!) - Date.parse(request.searchParams.get("start")!)).toBe(3600000);
  await expect(page).toHaveURL(/range=1h/);
  await page.getByRole("button", { name: "Expand log entry", exact: true }).click();
  await expect(page.getByLabel("Log entry details")).toContainText("workbench journal");
  await expect(page.locator(".bf-log-detail button")).toBeInViewport();
  await page.screenshot({ path: testInfo.outputPath("workbench-desktop.png") });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("button", { name: "Toggle fields sidebar", exact: true }).click();
  await expect(fields.getByRole("button", { name: "Close fields", exact: true })).toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
  await fields.getByRole("button", { name: "Close fields", exact: true }).click();
  await page.screenshot({ path: testInfo.outputPath("workbench-mobile.png") });
});
