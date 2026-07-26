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
  await expect(page.getByRole("button", { name: "Filter", exact: true })).toBeVisible();
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
