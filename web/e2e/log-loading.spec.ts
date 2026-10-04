import { expect, test } from "@playwright/test";

for (const kind of ["network-events", "system-logs"]) {
  test(`${kind} appends on scroll, retries and refreshes only on request`, async ({ page }) => {
    await page.setViewportSize({ width: 849, height: 824 });
    const offsets: number[] = [];
    let failNext = true;
    await page.route(`**/api/admin/${kind}?*`, async (route) => {
      const offset = Number(new URL(route.request().url()).searchParams.get("offset"));
      offsets.push(offset);
      if (offset === 25 && failNext) {
        failNext = false;
        await route.fulfill({ status: 500, json: { error: "temporary failure" } });
        return;
      }
      const rows = Array.from({ length: Math.min(25, 40 - offset) }, (_, index) => {
        const n = offset + index;
        const timestamp = new Date(Date.now() - n * 60_000).toISOString();
        return kind === "network-events" ? {
          node_name: "edge", user_name: `scroll-user-${n}`, auth_name: `auth-${n}`,
          source_ip: "192.0.2.1", target_host: "example.com", target_port: 443,
          action: "connect", raw_message: `scroll-row-${n}`, count: 1,
          window_start: timestamp, window_end: timestamp, created_at: timestamp
        } : { node: "edge", service: "sing-box", level: "info", message: `scroll-row-${n}`, observed_at: timestamp, ingested_at: timestamp };
      });
      await route.fulfill({ json: { total: 40, [kind === "network-events" ? "events" : "logs"]: rows, services: ["sing-box"] } });
    });
    await page.goto(kind);
    await expect(page.getByText("25 rows · 40 total", { exact: true })).toBeVisible();
    await expect(page.locator(".bf-page-topbar").getByRole("button", { name: "Refresh", exact: true })).toHaveCount(0);
    await expect(page.locator(".bf-page-topbar").getByRole("link", { name: "Logs", exact: true })).toHaveCount(0);
    await expect(page.getByRole("combobox", { name: "Page size", exact: true })).toHaveCount(0);
    const scroller = page.locator(".bf-table-scroll").first();
    await scroller.evaluate(element => { element.scrollTop = element.scrollHeight; });
    await expect(page.getByText("Could not load more logs.", { exact: true })).toBeVisible();
    await expect(page.getByText("25 rows · 40 total", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Retry", exact: true }).click();
    await expect(page.getByText("40 rows · 40 total", { exact: true })).toBeVisible();
    expect(offsets).toContain(25);
    await expect(page.getByRole("button", { name: kind === "network-events" ? "Expand network event" : "Expand log entry", exact: true })).toHaveCount(40);
    const before = offsets.length;
    await page.clock.install();
    await page.clock.runFor(31_000);
    await page.evaluate(() => window.dispatchEvent(new Event("focus")));
    await expect.poll(() => offsets.length).toBe(before);
    await page.getByRole("button", { name: "Log actions", exact: true }).click();
    await page.getByRole("menuitem", { name: "Refresh", exact: true }).click();
    await expect(page.getByText("25 rows · 40 total", { exact: true })).toBeVisible();
    expect(offsets.at(-1)).toBe(0);
  });
}

test("review and apply retains the success sweep in the notice row", async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 849, height: 824 });
  let published = false;
  await page.route("**/api/admin/config/changes", route => route.fulfill({ json: { changed: published ? [] : [{
    node: "animation-edge", target_hash: "old", rendered_hash: "new", target_version: "1", target_config: "{}", rendered_config: '{"log":{"level":"info"}}'
  }] } }));
  await page.route("**/api/admin/config/publish", route => {
    published = true;
    return route.fulfill({ json: { published: [{ node: "animation-edge", version: 2 }] } });
  });
  await page.route("**/api/admin/nodes", route => route.fulfill({ json: [{ name: "animation-edge", status: "active", apply_status: "applied", current_version: "2", target_version: "2" }] }));
  await page.goto("network-events");
  await page.getByRole("button", { name: "Review & apply", exact: true }).click();
  await page.getByRole("button", { name: "Publish & apply", exact: true }).click();
  const notice = page.locator(".bf-page-publish.publish-bar-unlock");
  await expect(notice).toBeVisible();
  expect(await notice.evaluate(element => getComputedStyle(element, "::after").animationName)).toBe("bf-slide-unlock");
  await expect.poll(() => notice.evaluate(element => {
    const sweep = element.getAnimations({ subtree: true }).find(animation => animation instanceof CSSAnimation && animation.animationName === "bf-slide-unlock");
    return Number(sweep?.currentTime ?? 0);
  })).toBeGreaterThanOrEqual(400);
  await page.screenshot({ path: testInfo.outputPath("publish-success.png") });
});

test("bulk credential changes remain readable and scroll to the last node", async ({ page }, testInfo) => {
  const changed = Array.from({ length: 20 }, (_, index) => ({
    node: `rotation-edge-${index + 1}`, target_hash: "old", rendered_hash: "new", target_version: "1",
    target_config: JSON.stringify({ users: Array.from({ length: 30 }, (_, n) => ({ name: `user-${n}`, uuid: "old-test-key" })) }, null, 2),
    rendered_config: JSON.stringify({ users: Array.from({ length: 30 }, (_, n) => ({ name: `user-${n}`, uuid: "new-test-key" })) }, null, 2)
  }));
  await page.route("**/api/admin/config/changes", route => route.fulfill({ json: { changed } }));
  let publishRequests = 0;
  await page.route("**/api/admin/config/publish", route => { publishRequests++; return route.fulfill({ json: { published: [] } }); });
  await page.goto("nodes");
  await page.getByRole("button", { name: "Review & apply", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Review configuration changes" });
  const list = dialog.locator(".overflow-y-auto");
  const cards = list.locator(":scope > div");
  await expect(cards).toHaveCount(20);
  for (const viewport of [{ width: 1440, height: 900 }, { width: 651, height: 824 }, { width: 390, height: 480 }]) {
    await page.setViewportSize(viewport);
    const geometry = await dialog.evaluate(el => {
      const bounds = el.getBoundingClientRect();
      const list = el.querySelector(".overflow-y-auto") as HTMLElement;
      return { top: bounds.top, bottom: bounds.bottom, left: bounds.left, right: bounds.right,
        scrollable: list.scrollHeight > list.clientHeight,
        heights: Array.from(list.children, child => child.getBoundingClientRect().height) };
    });
    expect(geometry.top).toBeGreaterThanOrEqual(0);
    expect(geometry.bottom).toBeLessThanOrEqual(viewport.height);
    expect(geometry.left).toBeGreaterThanOrEqual(0);
    expect(geometry.right).toBeLessThanOrEqual(viewport.width);
    expect(geometry.scrollable).toBeTruthy();
    expect(Math.min(...geometry.heights)).toBeGreaterThan(100);
    await expect(dialog.getByRole("button", { name: "Publish & apply", exact: true })).toBeInViewport();
    await list.evaluate(el => { el.scrollTop = el.scrollHeight; });
    await expect(dialog.getByText("rotation-edge-20", { exact: true })).toBeInViewport();
    await list.evaluate(el => { el.scrollTop = 0; });
  }
  await page.setViewportSize({ width: 651, height: 824 });
  await page.screenshot({ path: testInfo.outputPath("bulk-config-review.png") });
  await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(publishRequests).toBe(0);
});
